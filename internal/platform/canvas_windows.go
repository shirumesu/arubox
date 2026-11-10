package platform

import (
	"errors"
	"math"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/shirumesu/arubox/internal/design"
)

// A Canvas draws one desktop surface with Direct2D into a bitmap that the
// surface's layered window shows. It draws in software: the scene is small,
// a software target keeps no GPU memory and is never lost with the device.
// The bitmap is a WIC bitmap the window is updated from through GDI, which
// lends it to GDI without copying; a DC target bound to a GDI bitmap would
// keep a second screen-sized bitmap of its own. Canvases and what they make
// are used on the desktop's UI thread only.

var (
	d2d1                         = windows.NewLazySystemDLL("d2d1.dll")
	dwrite                       = windows.NewLazySystemDLL("dwrite.dll")
	procD2D1CreateFactory        = d2d1.NewProc("D2D1CreateFactory")
	procDWriteCreateFactory      = dwrite.NewProc("DWriteCreateFactory")
	procSelectObject             = gdi32.NewProc("SelectObject")
	procGetUserDefaultLocaleName = kernel32.NewProc("GetUserDefaultLocaleName")
)

var (
	iidID2D1Factory   = windows.GUID{Data1: 0x06152247, Data2: 0x6f50, Data3: 0x465a, Data4: [8]byte{0x92, 0x45, 0x11, 0x8b, 0xfd, 0x3b, 0x60, 0x07}}
	iidIDWriteFactory = windows.GUID{Data1: 0xb859ee5a, Data2: 0xd838, Data3: 0x4b5b, Data4: [8]byte{0xa2, 0xe8, 0x1a, 0xdc, 0x7d, 0x93, 0xdb, 0x48}}

	clsidWICImagingFactory       = windows.GUID{Data1: 0xcacaf262, Data2: 0x9370, Data3: 0x4615, Data4: [8]byte{0xa1, 0x3b, 0x9f, 0x55, 0x39, 0xda, 0x4c, 0x0a}}
	iidIWICImagingFactory        = windows.GUID{Data1: 0xec5ec8a9, Data2: 0xc395, Data3: 0x4314, Data4: [8]byte{0x9c, 0x77, 0x54, 0xd7, 0xa9, 0x35, 0xff, 0x70}}
	iidID2D1GdiInteropTarget     = windows.GUID{Data1: 0xe0db51c3, Data2: 0x6f77, Data3: 0x4bae, Data4: [8]byte{0xb3, 0xd5, 0xe4, 0x75, 0x09, 0xb3, 0x58, 0x38}}
	guidWICPixelFormat32bppPBGRA = windows.GUID{Data1: 0x6fddc324, Data2: 0x4e03, Data3: 0x4bfe, Data4: [8]byte{0xb1, 0x85, 0x3d, 0x77, 0x76, 0x8d, 0xc9, 0x10}}
)

// Virtual table slots.
const (
	slotFactoryCreateWicBitmapRenderTarget = 13 // ID2D1Factory

	slotWICCreateBitmap = 17 // IWICImagingFactory

	slotGdiGetDC     = 3 // ID2D1GdiInteropRenderTarget
	slotGdiReleaseDC = 4

	slotRTCreateBitmap          = 4 // ID2D1RenderTarget
	slotRTCreateSolidColorBrush = 8
	slotRTFillRectangle         = 17
	slotRTDrawRoundedRectangle  = 18
	slotRTFillRoundedRectangle  = 19
	slotRTDrawBitmap            = 26
	slotRTDrawTextLayout        = 28
	slotRTSetTransform          = 30
	slotRTSetTextAntialiasMode  = 34
	slotRTPushAxisAlignedClip   = 45
	slotRTPopAxisAlignedClip    = 46
	slotRTClear                 = 47
	slotRTBeginDraw             = 48
	slotRTEndDraw               = 49

	slotBrushSetColor = 8 // ID2D1SolidColorBrush

	slotDWCreateTextFormat           = 15 // IDWriteFactory
	slotDWCreateTextLayout           = 18
	slotDWCreateEllipsisTrimmingSign = 20

	slotTFSetTextAlignment      = 3 // IDWriteTextFormat
	slotTFSetParagraphAlignment = 4
	slotTFSetWordWrapping       = 5
	slotTFSetTrimming           = 9
	slotTFSetLineSpacing        = 10
	slotTLGetMetrics            = 60 // IDWriteTextLayout
)

const (
	dxgiFormatB8G8R8A8         = 87
	d2dAlphaPremultiplied      = 1
	d2dTargetSoftware          = 1
	d2dUsageGDICompatible      = 2
	d2dDCInitializeCopy        = 0
	wicBitmapCacheOnLoad       = 2
	d2dTextAntialiasGrayscale  = 2
	d2dAntialiasPerPrimitive   = 0
	d2dInterpolationLinear     = 1
	d2dDrawTextEnableColorFont = 4
	dwriteWeightNormal         = 400
	dwriteWeightSemiBold       = 600
	dwriteStretchNormal        = 5
	dwriteAlignLeading         = 0
	dwriteAlignCenter          = 2
	dwriteWrap                 = 0
	dwriteNoWrap               = 1
	dwriteTrimCharacter        = 1
	dwriteLineSpacingUniform   = 1
	d2dErrRecreateTarget       = 0x8899000C
)

type d2dColor struct{ R, G, B, A float32 }

type d2dRect struct{ Left, Top, Right, Bottom float32 }

type d2dRoundedRect struct {
	Rect             d2dRect
	RadiusX, RadiusY float32
}

type d2dPixelFormat struct{ Format, AlphaMode uint32 }

type d2dRenderTargetProperties struct {
	Type        uint32
	PixelFormat d2dPixelFormat
	DpiX, DpiY  float32
	Usage       uint32
	MinLevel    uint32
}

type d2dBitmapProperties struct {
	PixelFormat d2dPixelFormat
	DpiX, DpiY  float32
}

type d2dMatrix struct{ M11, M12, M21, M22, DX, DY float32 }

var (
	d2dFactory comObject
	dwFactory  comObject
	wicFactory comObject
)

// drawingFactories creates the Direct2D, DirectWrite and WIC factories
// once.
func drawingFactories() error {
	if d2dFactory != nil {
		return nil
	}
	var f comObject
	if hr, _, _ := procD2D1CreateFactory.Call(0, uintptr(unsafe.Pointer(&iidID2D1Factory)), 0, uintptr(unsafe.Pointer(&f))); !succeeded(hr) {
		return errors.New("创建 Direct2D 失败")
	}
	var dw comObject
	if hr, _, _ := procDWriteCreateFactory.Call(0, uintptr(unsafe.Pointer(&iidIDWriteFactory)), uintptr(unsafe.Pointer(&dw))); !succeeded(hr) {
		release(f)
		return errors.New("创建 DirectWrite 失败")
	}
	var wic comObject
	if hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidWICImagingFactory)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIWICImagingFactory)), uintptr(unsafe.Pointer(&wic))); !succeeded(hr) {
		release(f)
		release(dw)
		return errors.New("创建 WIC 失败")
	}
	d2dFactory, dwFactory, wicFactory = f, dw, wic
	return nil
}

// localeNameMax is LOCALE_NAME_MAX_LENGTH.
const localeNameMax = 85

func f32(v float64) uintptr { return uintptr(math.Float32bits(float32(v))) }

// point2F packs a D2D1_POINT_2F, which is passed by value in one register.
func point2F(x, y float64) uintptr {
	return uintptr(math.Float32bits(float32(x))) | uintptr(math.Float32bits(float32(y)))<<32
}

func toD2DRect(b Box) d2dRect {
	return d2dRect{float32(b.X), float32(b.Y), float32(b.X + b.Width), float32(b.Y + b.Height)}
}

func toD2DColor(c design.Color) d2dColor {
	return d2dColor{float32(c.R), float32(c.G), float32(c.B), float32(c.A)}
}

// Canvas is the drawing surface of one desktop surface. Coordinates are in
// physical pixels of the virtual screen.
type Canvas struct {
	bitmap     comObject // IWICBitmap
	target     comObject // ID2D1RenderTarget
	gdi        comObject // ID2D1GdiInteropRenderTarget
	brush      comObject
	width      int
	height     int
	originX    float64
	originY    float64
	generation int // bumps when the target is recreated, invalidating bitmaps
}

func newCanvas(width, height int, originX, originY float64) (*Canvas, error) {
	if err := drawingFactories(); err != nil {
		return nil, err
	}
	c := &Canvas{width: width, height: height, originX: originX, originY: originY}
	if !succeeded(call(wicFactory, slotWICCreateBitmap, uintptr(width), uintptr(height),
		uintptr(unsafe.Pointer(&guidWICPixelFormat32bppPBGRA)), wicBitmapCacheOnLoad, uintptr(unsafe.Pointer(&c.bitmap)))) {
		return nil, errors.New("创建绘图表面失败")
	}
	if err := c.createTarget(); err != nil {
		c.release()
		return nil, err
	}
	return c, nil
}

func (c *Canvas) createTarget() error {
	props := d2dRenderTargetProperties{
		Type:        d2dTargetSoftware,
		PixelFormat: d2dPixelFormat{dxgiFormatB8G8R8A8, d2dAlphaPremultiplied},
		DpiX:        96, DpiY: 96,
		Usage: d2dUsageGDICompatible,
	}
	var t comObject
	if !succeeded(call(d2dFactory, slotFactoryCreateWicBitmapRenderTarget, uintptr(unsafe.Pointer(c.bitmap)), uintptr(unsafe.Pointer(&props)), uintptr(unsafe.Pointer(&t)))) {
		return errors.New("创建 Direct2D 目标失败")
	}
	var gdi comObject
	if !succeeded(call(t, slotQueryInterface, uintptr(unsafe.Pointer(&iidID2D1GdiInteropTarget)), uintptr(unsafe.Pointer(&gdi)))) {
		release(t)
		return errors.New("创建 Direct2D 目标失败")
	}
	var brush comObject
	white := d2dColor{1, 1, 1, 1}
	if !succeeded(call(t, slotRTCreateSolidColorBrush, uintptr(unsafe.Pointer(&white)), 0, uintptr(unsafe.Pointer(&brush)))) {
		release(gdi)
		release(t)
		return errors.New("创建画笔失败")
	}
	call(t, slotRTSetTextAntialiasMode, d2dTextAntialiasGrayscale)
	c.target, c.gdi, c.brush = t, gdi, brush
	c.generation++
	return nil
}

func (c *Canvas) releaseTarget() {
	release(c.brush)
	release(c.gdi)
	release(c.target)
	c.brush, c.gdi, c.target = nil, nil, nil
}

func (c *Canvas) release() {
	c.releaseTarget()
	release(c.bitmap)
	c.bitmap = nil
}

// withDC lends the bitmap to GDI as a device context for f, outside
// drawing.
func (c *Canvas) withDC(f func(dc uintptr)) {
	call(c.target, slotRTBeginDraw)
	var dc uintptr
	if succeeded(call(c.gdi, slotGdiGetDC, d2dDCInitializeCopy, uintptr(unsafe.Pointer(&dc)))) {
		f(dc)
		// GDI only read the bitmap.
		var unchanged rect
		call(c.gdi, slotGdiReleaseDC, uintptr(unsafe.Pointer(&unchanged)))
	}
	call(c.target, slotRTEndDraw, 0, 0)
}

// begin starts drawing the dirty part, in virtual screen pixels, cleared to
// an almost transparent colour: a layered window passes clicks through
// fully transparent pixels, and the desktop must take every click.
func (c *Canvas) begin(dirty Box) {
	call(c.target, slotRTBeginDraw)
	m := d2dMatrix{M11: 1, M22: 1, DX: float32(-c.originX), DY: float32(-c.originY)}
	call(c.target, slotRTSetTransform, uintptr(unsafe.Pointer(&m)))
	r := toD2DRect(dirty)
	call(c.target, slotRTPushAxisAlignedClip, uintptr(unsafe.Pointer(&r)), d2dAntialiasPerPrimitive)
	clear := d2dColor{0, 0, 0, 0}
	call(c.target, slotRTClear, uintptr(unsafe.Pointer(&clear)))
	c.FillRect(dirty, design.Color{A: 1.0 / 255})
}

// end finishes drawing. A lost target is recreated, and the caller draws
// everything again.
func (c *Canvas) end() (redraw bool, err error) {
	call(c.target, slotRTPopAxisAlignedClip)
	hr := call(c.target, slotRTEndDraw, 0, 0)
	if uint32(hr) == d2dErrRecreateTarget {
		c.releaseTarget()
		return true, c.createTarget()
	}
	if !succeeded(hr) {
		return false, errors.New("Direct2D 绘制失败")
	}
	return false, nil
}

func (c *Canvas) color(col design.Color) uintptr {
	d := toD2DColor(col)
	call(c.brush, slotBrushSetColor, uintptr(unsafe.Pointer(&d)))
	return uintptr(c.brush)
}

// FillRect fills a rectangle.
func (c *Canvas) FillRect(b Box, col design.Color) {
	r := toD2DRect(b)
	call(c.target, slotRTFillRectangle, uintptr(unsafe.Pointer(&r)), c.color(col))
}

// FillRoundRect fills a rectangle with rounded corners.
func (c *Canvas) FillRoundRect(b Box, radius float64, col design.Color) {
	r := d2dRoundedRect{toD2DRect(b), float32(radius), float32(radius)}
	call(c.target, slotRTFillRoundedRectangle, uintptr(unsafe.Pointer(&r)), c.color(col))
}

// StrokeRoundRect draws the outline of a rectangle with rounded corners,
// centred on its edge.
func (c *Canvas) StrokeRoundRect(b Box, radius, width float64, col design.Color) {
	r := d2dRoundedRect{toD2DRect(b), float32(radius), float32(radius)}
	call(c.target, slotRTDrawRoundedRectangle, uintptr(unsafe.Pointer(&r)), c.color(col), f32(width), 0)
}

// PushClip limits drawing to a rectangle until PopClip.
func (c *Canvas) PushClip(b Box) {
	r := toD2DRect(b)
	call(c.target, slotRTPushAxisAlignedClip, uintptr(unsafe.Pointer(&r)), d2dAntialiasPerPrimitive)
}

// PopClip undoes the last PushClip.
func (c *Canvas) PopClip() { call(c.target, slotRTPopAxisAlignedClip) }

// Bitmap is a picture ready to draw on a canvas.
type Bitmap struct {
	obj        comObject
	generation int
	Width      int
	Height     int
}

// NewBitmap makes a bitmap from BGRA pixels with premultiplied alpha.
func (c *Canvas) NewBitmap(img Image) *Bitmap {
	if img.Width == 0 || img.Height == 0 {
		return nil
	}
	props := d2dBitmapProperties{PixelFormat: d2dPixelFormat{dxgiFormatB8G8R8A8, d2dAlphaPremultiplied}, DpiX: 96, DpiY: 96}
	size := uintptr(uint32(img.Width)) | uintptr(uint32(img.Height))<<32
	var b comObject
	if !succeeded(call(c.target, slotRTCreateBitmap, size, uintptr(unsafe.Pointer(&img.Pixels[0])), uintptr(img.Width*4), uintptr(unsafe.Pointer(&props)), uintptr(unsafe.Pointer(&b)))) {
		return nil
	}
	return &Bitmap{obj: b, generation: c.generation, Width: img.Width, Height: img.Height}
}

// Usable reports whether the bitmap belongs to the canvas's current target.
func (c *Canvas) Usable(b *Bitmap) bool { return b != nil && b.generation == c.generation }

// Release frees the bitmap.
func (b *Bitmap) Release() {
	if b != nil {
		release(b.obj)
		b.obj = nil
	}
}

// DrawBitmap draws a bitmap scaled into a rectangle.
func (c *Canvas) DrawBitmap(b *Bitmap, dst Box, opacity float64) {
	r := toD2DRect(dst)
	call(c.target, slotRTDrawBitmap, uintptr(b.obj), uintptr(unsafe.Pointer(&r)), f32(opacity), d2dInterpolationLinear, 0)
}

// Font describes how text is set.
type Font struct {
	Family string
	// Size and LineHeight are in pixels.
	Size       float64
	LineHeight float64
	Bold       bool
	Center     bool
	// Lines limits the text to this many lines, ending the last with an
	// ellipsis; 1 keeps it on one line.
	Lines int
}

// TextLayout is text set within a width.
type TextLayout struct {
	obj    comObject
	Width  float64 // of the widest line
	Height float64
}

// Release frees the layout.
func (t *TextLayout) Release() {
	if t != nil {
		release(t.obj)
		t.obj = nil
	}
}

var textFormats = map[Font]comObject{}

func textFormat(f Font) (comObject, error) {
	if tf, ok := textFormats[f]; ok {
		return tf, nil
	}
	if err := drawingFactories(); err != nil {
		return nil, err
	}
	family, err := windows.UTF16PtrFromString(f.Family)
	if err != nil {
		return nil, err
	}
	// The user's locale picks the glyphs of characters several scripts
	// share, such as Chinese ones over their Japanese forms.
	var locale [localeNameMax]uint16
	procGetUserDefaultLocaleName.Call(uintptr(unsafe.Pointer(&locale[0])), localeNameMax)
	weight := uintptr(dwriteWeightNormal)
	if f.Bold {
		weight = dwriteWeightSemiBold
	}
	var tf comObject
	if !succeeded(call(dwFactory, slotDWCreateTextFormat, uintptr(unsafe.Pointer(family)), 0, weight, 0, dwriteStretchNormal,
		f32(f.Size), uintptr(unsafe.Pointer(&locale[0])), uintptr(unsafe.Pointer(&tf)))) {
		return nil, errors.New("创建字体失败")
	}
	align := uintptr(dwriteAlignLeading)
	if f.Center {
		align = dwriteAlignCenter
	}
	call(tf, slotTFSetTextAlignment, align)
	wrap := uintptr(dwriteWrap)
	if f.Lines == 1 {
		wrap = dwriteNoWrap
	}
	call(tf, slotTFSetWordWrapping, wrap)
	call(tf, slotTFSetLineSpacing, dwriteLineSpacingUniform, f32(f.LineHeight), f32(f.LineHeight*0.8))
	var sign comObject
	if succeeded(call(dwFactory, slotDWCreateEllipsisTrimmingSign, uintptr(tf), uintptr(unsafe.Pointer(&sign)))) {
		trimming := struct{ Granularity, Delimiter, DelimiterCount uint32 }{Granularity: dwriteTrimCharacter}
		call(tf, slotTFSetTrimming, uintptr(unsafe.Pointer(&trimming)), uintptr(sign))
		release(sign)
	}
	textFormats[f] = tf
	return tf, nil
}

// NewTextLayout sets text within a width, on up to the font's number of
// lines.
func NewTextLayout(text string, f Font, width float64) *TextLayout {
	tf, err := textFormat(f)
	if err != nil {
		return nil
	}
	s, err := windows.UTF16FromString(text)
	if err != nil || len(s) <= 1 {
		return nil
	}
	height := 1e5
	if f.Lines > 0 {
		height = float64(f.Lines) * f.LineHeight
	}
	var l comObject
	if !succeeded(call(dwFactory, slotDWCreateTextLayout, uintptr(unsafe.Pointer(&s[0])), uintptr(len(s)-1), uintptr(tf),
		f32(width), f32(height), uintptr(unsafe.Pointer(&l)))) {
		return nil
	}
	var m struct {
		Left, Top, Width, WidthTrailing, Height, LayoutWidth, LayoutHeight float32
		MaxBidiDepth, LineCount                                            uint32
	}
	call(l, slotTLGetMetrics, uintptr(unsafe.Pointer(&m)))
	return &TextLayout{obj: l, Width: float64(m.Width), Height: math.Min(float64(m.Height), height)}
}

// DrawText draws a text layout with its top left corner at x, y.
func (c *Canvas) DrawText(t *TextLayout, x, y float64, col design.Color) {
	call(c.target, slotRTDrawTextLayout, point2F(x, y), uintptr(t.obj), c.color(col), d2dDrawTextEnableColorFont)
}
