package ui

import (
	"errors"
	"math"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/shirumesu/arubox/internal/design"
	"github.com/shirumesu/arubox/internal/pixellock"
)

// Color is a colour as the design tokens define it.
type Color = design.Color

// RGBA makes a colour from 0–255 parts and an alpha from 0 to 1.
func RGBA(r, g, b uint8, a float64) Color {
	return Color{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255, A: a}
}

// Rect is a rectangle in device-independent pixels (96 per inch).
type Rect struct{ X, Y, W, H float64 }

// Point is a position in device-independent pixels.
type Point struct{ X, Y float64 }

// Right returns the right edge.
func (r Rect) Right() float64 { return r.X + r.W }

// Bottom returns the bottom edge.
func (r Rect) Bottom() float64 { return r.Y + r.H }

// Contains reports whether p is inside r.
func (r Rect) Contains(p Point) bool {
	return p.X >= r.X && p.X < r.X+r.W && p.Y >= r.Y && p.Y < r.Y+r.H
}

// Inset shrinks r by d on every side.
func (r Rect) Inset(d float64) Rect { return Rect{r.X + d, r.Y + d, r.W - 2*d, r.H - 2*d} }

// InsetXY shrinks r by dx left and right and dy top and bottom.
func (r Rect) InsetXY(dx, dy float64) Rect { return Rect{r.X + dx, r.Y + dy, r.W - 2*dx, r.H - 2*dy} }

// Intersect returns the part of r inside o.
func (r Rect) Intersect(o Rect) Rect {
	x0, y0 := math.Max(r.X, o.X), math.Max(r.Y, o.Y)
	x1, y1 := math.Min(r.Right(), o.Right()), math.Min(r.Bottom(), o.Bottom())
	if x1 <= x0 || y1 <= y0 {
		return Rect{}
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// Empty reports whether r has no area.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// CutTop splits off the top h of r and returns it with the rest.
func (r Rect) CutTop(h float64) (top, rest Rect) {
	h = math.Min(h, r.H)
	return Rect{r.X, r.Y, r.W, h}, Rect{r.X, r.Y + h, r.W, r.H - h}
}

// CutLeft splits off the left w of r and returns it with the rest.
func (r Rect) CutLeft(w float64) (left, rest Rect) {
	w = math.Min(w, r.W)
	return Rect{r.X, r.Y, w, r.H}, Rect{r.X + w, r.Y, r.W - w, r.H}
}

// CutRight splits off the right w of r and returns it with the rest.
func (r Rect) CutRight(w float64) (right, rest Rect) {
	w = math.Min(w, r.W)
	return Rect{r.Right() - w, r.Y, w, r.H}, Rect{r.X, r.Y, r.W - w, r.H}
}

// CutBottom splits off the bottom h of r and returns it with the rest.
func (r Rect) CutBottom(h float64) (bottom, rest Rect) {
	h = math.Min(h, r.H)
	return Rect{r.X, r.Bottom() - h, r.W, h}, Rect{r.X, r.Y, r.W, r.H - h}
}

// Center returns a w×h rectangle centred in r.
func (r Rect) Center(w, h float64) Rect {
	return Rect{r.X + (r.W-w)/2, r.Y + (r.H-h)/2, w, h}
}

// Weight is a font weight.
type Weight int

// Font weights.
const (
	Regular  Weight = 400
	Medium   Weight = 500
	Semibold Weight = 600
	Bold     Weight = 700
)

// Align places text horizontally.
type Align int

// Text alignments.
const (
	AlignStart Align = iota
	AlignEnd
	AlignCenter
)

// Text describes how text is set.
type Text struct {
	Size   float64 // in DIPs; 0 means 14
	Weight Weight  // 0 means Regular
	Color  Color
	Align  Align
	// Middle centres the text vertically in its rectangle; otherwise it
	// starts at the top.
	Middle bool
	// Wrap lets the text run over several lines; otherwise it stays on
	// one line and ends with an ellipsis when it does not fit.
	Wrap bool
	// Family overrides the design's font; "Consolas" for code, say.
	Family string
	// LineHeight in DIPs; 0 means the font's own.
	LineHeight float64
}

type formatKey struct {
	family     string
	size       float64
	weight     Weight
	align      Align
	middle     bool
	wrap       bool
	lineHeight float64
}

var formats = map[formatKey]comObject{}

func textFormat(k formatKey) comObject {
	if f, ok := formats[k]; ok {
		return f
	}
	family := k.family
	if family == "" {
		family = design.Fonts[0]
	}
	var f comObject
	if !succeeded(callf(dwFactory, slotDWCreateTextFormat, "iiiiifii", utf16Ptr(family), 0, uintptr(k.weight), 0, dwriteStretchNormal,
		f32(k.size), utf16Ptr("zh-CN"), uintptr(unsafe.Pointer(&f)))) {
		return nil
	}
	call(f, slotTFSetTextAlignment, uintptr([]int{0, 1, 2}[k.align]))
	if k.middle {
		call(f, slotTFSetParagraphAlignment, 2)
	}
	if k.wrap {
		call(f, slotTFSetWordWrapping, dwriteWrap)
	} else {
		call(f, slotTFSetWordWrapping, dwriteNoWrap)
		var sign comObject
		if succeeded(call(dwFactory, slotDWCreateEllipsisTrimmingSign, uintptr(f), uintptr(unsafe.Pointer(&sign)))) {
			trimming := struct{ Granularity, Delimiter, DelimiterCount uint32 }{Granularity: dwriteTrimCharacter}
			call(f, slotTFSetTrimming, uintptr(unsafe.Pointer(&trimming)), uintptr(sign))
			release(sign)
		}
	}
	if k.lineHeight > 0 {
		callf(f, slotTFSetLineSpacing, "iff", dwriteLineSpacingUniform, f32(k.lineHeight), f32(k.lineHeight*0.8))
	}
	formats[k] = f
	return f
}

func (t Text) key() formatKey {
	k := formatKey{family: t.Family, size: t.Size, weight: t.Weight, align: t.Align, middle: t.Middle, wrap: t.Wrap, lineHeight: t.LineHeight}
	if k.size == 0 {
		k.size = 14
	}
	if k.weight == 0 {
		k.weight = Regular
	}
	return k
}

type textMetrics struct {
	Left, Top, Width, WidthTrailing, Height, LayoutWidth, LayoutHeight float32
	MaxBidiDepth, LineCount                                            uint32
}

func textLayout(s string, t Text, w, h float64) comObject {
	if s == "" || factories() != nil {
		return nil
	}
	f := textFormat(t.key())
	if f == nil {
		return nil
	}
	u, err := windows.UTF16FromString(s)
	if err != nil || len(u) <= 1 {
		return nil
	}
	var l comObject
	if !succeeded(callf(dwFactory, slotDWCreateTextLayout, "iiiffi", uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(f),
		f32(math.Max(w, 0)), f32(math.Max(h, 0)), uintptr(unsafe.Pointer(&l)))) {
		return nil
	}
	return l
}

// MeasureText returns the size text takes when set within maxWidth (0 for
// no limit).
func MeasureText(s string, t Text, maxWidth float64) (w, h float64) {
	if maxWidth <= 0 {
		maxWidth = 1e5
	}
	if !t.Wrap {
		t.Wrap = false
	}
	l := textLayout(s, t, maxWidth, 1e5)
	if l == nil {
		return 0, t.key().size * 1.33
	}
	defer release(l)
	var m textMetrics
	call(l, slotTLGetMetrics, uintptr(unsafe.Pointer(&m)))
	return float64(m.WidthTrailing), float64(m.Height)
}

// Canvas draws into a window or an image. Coordinates are in DIPs.
type Canvas struct {
	target comObject // ID2D1RenderTarget
	brush  comObject
	gen    int // bumps whenever target is recreated
	clips  int
	// Scale is physical pixels per DIP.
	Scale float64
	// Theme is the palette the window draws with.
	Theme *design.Palette
	// origin is the current translation.
	ox, oy float64
	// images holds the images with a bitmap on target. A bitmap keeps its
	// target alive, so they are released with it.
	images map[*Image]struct{}
}

func newCanvasOn(t comObject, gen int) (*Canvas, error) {
	c := &Canvas{target: t, gen: gen, Scale: 1}
	white := d2dColor{1, 1, 1, 1}
	if !succeeded(call(t, slotRTCreateSolidColorBrush, uintptr(unsafe.Pointer(&white)), 0, uintptr(unsafe.Pointer(&c.brush)))) {
		return nil, errors.New("创建画笔失败")
	}
	call(t, slotRTSetTextAntialiasMode, d2dTextAntialiasGray)
	return c, nil
}

func (c *Canvas) release() {
	for img := range c.images {
		img.Release()
	}
	release(c.brush)
	c.brush = nil
}

func (c *Canvas) color(col Color) uintptr {
	d := d2dColor{float32(col.R), float32(col.G), float32(col.B), float32(col.A)}
	call(c.brush, slotBrushSetColor, uintptr(unsafe.Pointer(&d)))
	return uintptr(c.brush)
}

func d2r(r Rect) d2dRect {
	return d2dRect{float32(r.X), float32(r.Y), float32(r.X + r.W), float32(r.Y + r.H)}
}

// Clear fills everything with a colour.
func (c *Canvas) Clear(col Color) {
	d := d2dColor{float32(col.R), float32(col.G), float32(col.B), float32(col.A)}
	call(c.target, slotRTClear, uintptr(unsafe.Pointer(&d)))
}

// Fill fills a rectangle.
func (c *Canvas) Fill(r Rect, col Color) {
	if col.A <= 0 {
		return
	}
	d := d2r(r)
	call(c.target, slotRTFillRectangle, uintptr(unsafe.Pointer(&d)), c.color(col))
}

// FillRound fills a rectangle with rounded corners.
func (c *Canvas) FillRound(r Rect, radius float64, col Color) {
	if col.A <= 0 {
		return
	}
	d := d2dRoundedRect{d2r(r), float32(radius), float32(radius)}
	call(c.target, slotRTFillRoundedRectangle, uintptr(unsafe.Pointer(&d)), c.color(col))
}

// StrokeRound outlines a rounded rectangle inside its edge.
func (c *Canvas) StrokeRound(r Rect, radius, width float64, col Color) {
	if col.A <= 0 {
		return
	}
	d := d2dRoundedRect{d2r(r.Inset(width / 2)), float32(radius), float32(radius)}
	callf(c.target, slotRTDrawRoundedRectangle, "iifi", uintptr(unsafe.Pointer(&d)), c.color(col), f32(width), 0)
}

// Stroke outlines a rectangle inside its edge; dashed draws it dashed.
func (c *Canvas) Stroke(r Rect, width float64, col Color, dashed bool) {
	d := d2r(r.Inset(width / 2))
	style := uintptr(0)
	if dashed {
		style = uintptr(dashStroke)
	}
	callf(c.target, slotRTDrawRectangle, "iifi", uintptr(unsafe.Pointer(&d)), c.color(col), f32(width), style)
}

// Line draws a line with round ends.
func (c *Canvas) Line(a, b Point, width float64, col Color) {
	callf(c.target, slotRTDrawLine, "ppifi", point2F(a.X, a.Y), point2F(b.X, b.Y), c.color(col), f32(width), uintptr(roundStroke))
}

// FillEllipse fills an ellipse inside r.
func (c *Canvas) FillEllipse(r Rect, col Color) {
	e := d2dEllipse{float32(r.X + r.W/2), float32(r.Y + r.H/2), float32(r.W / 2), float32(r.H / 2)}
	call(c.target, slotRTFillEllipse, uintptr(unsafe.Pointer(&e)), c.color(col))
}

// StrokeEllipse outlines an ellipse inside r.
func (c *Canvas) StrokeEllipse(r Rect, width float64, col Color) {
	r = r.Inset(width / 2)
	e := d2dEllipse{float32(r.X + r.W/2), float32(r.Y + r.H/2), float32(r.W / 2), float32(r.H / 2)}
	callf(c.target, slotRTDrawEllipse, "iifi", uintptr(unsafe.Pointer(&e)), c.color(col), f32(width), 0)
}

// path builds a geometry through points; closed joins the last to the
// first.
func path(points []Point, closed, filled bool) comObject {
	if len(points) < 2 {
		return nil
	}
	var g comObject
	if !succeeded(call(d2dFactory, slotFactoryCreatePathGeometry, uintptr(unsafe.Pointer(&g)))) {
		return nil
	}
	var sink comObject
	if !succeeded(call(g, slotPathOpen, uintptr(unsafe.Pointer(&sink)))) {
		release(g)
		return nil
	}
	begin := uintptr(d2dFigureBeginHollow)
	if filled {
		begin = d2dFigureBeginFilled
	}
	callf(sink, slotSinkBeginFigure, "pi", point2F(points[0].X, points[0].Y), begin)
	rest := make([]float32, 0, 2*(len(points)-1))
	for _, p := range points[1:] {
		rest = append(rest, float32(p.X), float32(p.Y))
	}
	call(sink, slotSinkAddLines, uintptr(unsafe.Pointer(&rest[0])), uintptr(len(points)-1))
	end := uintptr(d2dFigureEndOpen)
	if closed {
		end = d2dFigureEndClosed
	}
	call(sink, slotSinkEndFigure, end)
	call(sink, slotSinkClose)
	release(sink)
	return g
}

// Polyline draws connected lines with round joins and ends.
func (c *Canvas) Polyline(points []Point, width float64, col Color) {
	g := path(points, false, false)
	if g == nil {
		return
	}
	defer release(g)
	callf(c.target, slotRTDrawGeometry, "iifi", uintptr(g), c.color(col), f32(width), uintptr(roundStroke))
}

// FillPolygon fills the polygon through points.
func (c *Canvas) FillPolygon(points []Point, col Color) {
	g := path(points, true, true)
	if g == nil {
		return
	}
	defer release(g)
	call(c.target, slotRTFillGeometry, uintptr(g), c.color(col), 0)
}

// Text draws s in r.
func (c *Canvas) Text(r Rect, s string, t Text) {
	l := textLayout(s, t, r.W, r.H)
	if l == nil {
		return
	}
	defer release(l)
	callf(c.target, slotRTDrawTextLayout, "piii", point2F(r.X, r.Y), uintptr(l), c.color(t.Color), d2dDrawTextEnableColorFont|d2dDrawTextClip)
}

// PushClip limits drawing to r until PopClip.
func (c *Canvas) PushClip(r Rect) {
	d := d2r(r)
	call(c.target, slotRTPushAxisAlignedClip, uintptr(unsafe.Pointer(&d)), d2dAntialiasPerPrimitive)
	c.clips++
}

// PopClip undoes the last PushClip.
func (c *Canvas) PopClip() {
	if c.clips > 0 {
		call(c.target, slotRTPopAxisAlignedClip)
		c.clips--
	}
}

// Translate moves the origin by dx, dy until Translate is called back.
func (c *Canvas) Translate(dx, dy float64) {
	c.ox += dx
	c.oy += dy
	c.setTransform(1)
}

// Transform sets a scale about the origin on top of the translation.
func (c *Canvas) setTransform(scale float64) {
	m := d2dMatrix{M11: float32(scale), M22: float32(scale), DX: float32(c.ox), DY: float32(c.oy)}
	call(c.target, slotRTSetTransform, uintptr(unsafe.Pointer(&m)))
}

// Zoom draws f with everything scaled by s about (ox, oy) in the current
// coordinates.
func (c *Canvas) Zoom(s, ox, oy float64, f func()) {
	saveX, saveY := c.ox, c.oy
	c.ox, c.oy = c.ox+ox, c.oy+oy
	m := d2dMatrix{M11: float32(s), M22: float32(s), DX: float32(c.ox), DY: float32(c.oy)}
	call(c.target, slotRTSetTransform, uintptr(unsafe.Pointer(&m)))
	f()
	c.ox, c.oy = saveX, saveY
	c.setTransform(1)
}

// Image is a picture in premultiplied BGRA pixels, drawable on any canvas.
type Image struct {
	Width, Height int
	Pixels        []byte
	// the bitmap made for one canvas target, remade when the target
	// changes
	bitmap comObject
	owner  comObject
	gen    int
	canvas *Canvas
}

// NewImage wraps premultiplied BGRA pixels; it keeps the slice.
func NewImage(width, height int, pixels []byte) *Image {
	return &Image{Width: width, Height: height, Pixels: pixels}
}

// Release frees what drawing the image created. Call on the UI thread.
func (img *Image) Release() {
	if img != nil && img.bitmap != nil {
		release(img.bitmap)
		img.bitmap = nil
		delete(img.canvas.images, img)
		img.canvas = nil
	}
}

// Changed tells the image its pixels were modified.
func (img *Image) Changed() { img.Release() }

func (c *Canvas) bitmapOf(img *Image) comObject {
	if img.bitmap != nil && img.owner == c.target && img.gen == c.gen {
		return img.bitmap
	}
	img.Release()
	if img.Width == 0 || img.Height == 0 || len(img.Pixels) < img.Width*img.Height*4 {
		return nil
	}
	// A shared bitmap reads the image's own pixels. One made by copying
	// them would land in the software rasterizer's pool, which keeps its
	// buffers for the life of the process.
	props := d2dBitmapProperties{PixelFormat: d2dPixelFormat{dxgiFormatB8G8R8A8, d2dAlphaPremultiplied}, DpiX: 96, DpiY: 96}
	lock := pixellock.New(img.Pixels, img.Width, img.Height)
	var b comObject
	hr := call(c.target, slotRTCreateSharedBitmap, uintptr(unsafe.Pointer(&pixellock.IID)), lock, uintptr(unsafe.Pointer(&props)), uintptr(unsafe.Pointer(&b)))
	pixellock.Release(lock)
	if !succeeded(hr) {
		return nil
	}
	img.bitmap, img.owner, img.gen, img.canvas = b, c.target, c.gen, c
	if c.images == nil {
		c.images = map[*Image]struct{}{}
	}
	c.images[img] = struct{}{}
	return b
}

// Image draws the part src of img (in image pixels; empty means all of
// it) scaled into dst. smooth picks linear rather than nearest-neighbour
// filtering.
func (c *Canvas) Image(img *Image, dst, src Rect, opacity float64, smooth bool) {
	if img == nil {
		return
	}
	b := c.bitmapOf(img)
	if b == nil {
		return
	}
	d := d2r(dst)
	mode := uintptr(d2dInterpolationNearest)
	if smooth {
		mode = d2dInterpolationLinear
	}
	var sp uintptr
	var s d2dRect
	if !src.Empty() {
		s = d2r(src)
		sp = uintptr(unsafe.Pointer(&s))
	}
	callf(c.target, slotRTDrawBitmap, "iifii", uintptr(b), uintptr(unsafe.Pointer(&d)), f32(opacity), mode, sp)
}

// Render draws with f into a new width×height image in physical pixels,
// on a transparent background, in software. It is how annotations are
// flattened into a screenshot with the same code that shows them.
func Render(width, height int, f func(c *Canvas)) (*Image, error) {
	if err := factories(); err != nil {
		return nil, err
	}
	var bmp comObject
	if !succeeded(call(wicFactory, slotWICCreateBitmap, uintptr(width), uintptr(height),
		uintptr(unsafe.Pointer(&guidWICPixelFormat32bppPBGRA)), wicBitmapCacheOnLoad, uintptr(unsafe.Pointer(&bmp)))) {
		return nil, errors.New("创建位图失败")
	}
	defer release(bmp)
	props := d2dRenderTargetProperties{
		Type:        d2dTargetSoftware,
		PixelFormat: d2dPixelFormat{dxgiFormatB8G8R8A8, d2dAlphaPremultiplied},
		DpiX:        96, DpiY: 96,
	}
	var t comObject
	if !succeeded(call(d2dFactory, slotFactoryCreateWicBitmapRenderTarget, uintptr(unsafe.Pointer(bmp)), uintptr(unsafe.Pointer(&props)), uintptr(unsafe.Pointer(&t)))) {
		return nil, errors.New("创建绘图目标失败")
	}
	defer release(t)
	c, err := newCanvasOn(t, 1)
	if err != nil {
		return nil, err
	}
	defer c.release()
	call(t, slotRTBeginDraw)
	c.Clear(Color{})
	f(c)
	for c.clips > 0 {
		c.PopClip()
	}
	if hr := call(t, slotRTEndDraw, 0, 0); !succeeded(hr) {
		return nil, errors.New("绘制失败")
	}
	px := make([]byte, width*height*4)
	if !succeeded(call(bmp, slotWICBitmapCopyPixels, 0, uintptr(width*4), uintptr(len(px)), uintptr(unsafe.Pointer(&px[0])))) {
		return nil, errors.New("读取位图失败")
	}
	return NewImage(width, height, px), nil
}

// dwriteHitTestMetrics is DWRITE_HIT_TEST_METRICS.
type dwriteHitTestMetrics struct {
	TextPosition, Length     uint32
	Left, Top, Width, Height float32
	BidiLevel                uint32
	IsText, IsTrimmed        int32
}

// TextRanges returns where the text from start to end, in UTF-16 units,
// lies when s is drawn with Text into r: one rectangle per line it spans.
// For start == end it returns the caret, a rectangle of no width.
func TextRanges(r Rect, s string, t Text, start, end int) []Rect {
	probe := s
	if probe == "" {
		probe = " " // an empty layout has no line to put the caret on
	}
	l := textLayout(probe, t, r.W, r.H)
	if l == nil {
		return nil
	}
	defer release(l)
	if start == end {
		var x, y float32
		var m dwriteHitTestMetrics
		call(l, slotTLHitTestTextPosition, uintptr(start), 0, uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y)), uintptr(unsafe.Pointer(&m)))
		return []Rect{{X: r.X + float64(x), Y: r.Y + float64(m.Top), H: float64(m.Height)}}
	}
	if start > end {
		start, end = end, start
	}
	ms := make([]dwriteHitTestMetrics, 16)
	var n uint32
	callf(l, slotTLHitTestTextRange, "iiffiii", uintptr(start), uintptr(end-start), f32(0), f32(0), uintptr(unsafe.Pointer(&ms[0])), uintptr(len(ms)), uintptr(unsafe.Pointer(&n)))
	if int(n) > len(ms) {
		ms = make([]dwriteHitTestMetrics, n)
		callf(l, slotTLHitTestTextRange, "iiffiii", uintptr(start), uintptr(end-start), f32(0), f32(0), uintptr(unsafe.Pointer(&ms[0])), uintptr(len(ms)), uintptr(unsafe.Pointer(&n)))
	}
	out := make([]Rect, 0, n)
	for _, m := range ms[:min(int(n), len(ms))] {
		out = append(out, Rect{X: r.X + float64(m.Left), Y: r.Y + float64(m.Top), W: float64(m.Width), H: float64(m.Height)})
	}
	return out
}
