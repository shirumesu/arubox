package ui

import (
	"errors"
	"math"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// comObject is a COM interface pointer.
type comObject unsafe.Pointer

// call invokes the method in the given slot of the object's virtual table.
// The directive keeps what pointer arguments point to on the heap for the
// call, so a growing stack cannot move an out-parameter.
//
//go:uintptrescapes
func call(o comObject, slot int, args ...uintptr) uintptr {
	vtable := *(*unsafe.Pointer)(o)
	method := *(*uintptr)(unsafe.Add(vtable, uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(method, append([]uintptr{uintptr(o)}, args...)...)
	return r
}

func release(o comObject) {
	if o != nil {
		call(o, 2)
	}
}

func succeeded(hr uintptr) bool { return int32(hr) >= 0 }

var (
	d2d1                    = windows.NewLazySystemDLL("d2d1.dll")
	dwrite                  = windows.NewLazySystemDLL("dwrite.dll")
	procD2D1CreateFactory   = d2d1.NewProc("D2D1CreateFactory")
	procDWriteCreateFactory = dwrite.NewProc("DWriteCreateFactory")

	iidID2D1Factory              = windows.GUID{Data1: 0x06152247, Data2: 0x6f50, Data3: 0x465a, Data4: [8]byte{0x92, 0x45, 0x11, 0x8b, 0xfd, 0x3b, 0x60, 0x07}}
	iidIDWriteFactory            = windows.GUID{Data1: 0xb859ee5a, Data2: 0xd838, Data3: 0x4b5b, Data4: [8]byte{0xa2, 0xe8, 0x1a, 0xdc, 0x7d, 0x93, 0xdb, 0x48}}
	clsidWICImagingFactory       = windows.GUID{Data1: 0xcacaf262, Data2: 0x9370, Data3: 0x4615, Data4: [8]byte{0xa1, 0x3b, 0x9f, 0x55, 0x39, 0xda, 0x4c, 0x0a}}
	iidIWICImagingFactory        = windows.GUID{Data1: 0xec5ec8a9, Data2: 0xc395, Data3: 0x4314, Data4: [8]byte{0x9c, 0x77, 0x54, 0xd7, 0xa9, 0x35, 0xff, 0x70}}
	guidWICPixelFormat32bppPBGRA = windows.GUID{Data1: 0x6fddc324, Data2: 0x4e03, Data3: 0x4bfe, Data4: [8]byte{0xb1, 0x85, 0x3d, 0x77, 0x76, 0x8d, 0xc9, 0x10}}
)

// Virtual table slots.
const (
	slotFactoryCreatePathGeometry          = 10
	slotFactoryCreateStrokeStyle           = 11
	slotFactoryCreateWicBitmapRenderTarget = 13
	slotFactoryCreateDCRenderTarget        = 16

	slotRTCreateBitmap          = 4
	slotRTCreateSharedBitmap    = 6
	slotRTCreateSolidColorBrush = 8
	slotRTDrawLine              = 15
	slotRTDrawRectangle         = 16
	slotRTFillRectangle         = 17
	slotRTDrawRoundedRectangle  = 18
	slotRTFillRoundedRectangle  = 19
	slotRTDrawEllipse           = 20
	slotRTFillEllipse           = 21
	slotRTDrawGeometry          = 22
	slotRTFillGeometry          = 23
	slotRTDrawBitmap            = 26
	slotRTDrawTextLayout        = 28
	slotRTSetTransform          = 30
	slotRTGetTransform          = 31
	slotRTSetTextAntialiasMode  = 34
	slotRTPushAxisAlignedClip   = 45
	slotRTPopAxisAlignedClip    = 46
	slotRTClear                 = 47
	slotRTBeginDraw             = 48
	slotRTEndDraw               = 49
	slotRTSetDpi                = 51
	slotDCRTBindDC              = 57

	slotBrushSetColor = 8

	slotPathOpen        = 17
	slotSinkSetFillMode = 3
	slotSinkBeginFigure = 5
	slotSinkAddLines    = 6
	slotSinkEndFigure   = 8
	slotSinkClose       = 9
	slotSinkAddBezier   = 11

	slotDWCreateTextFormat           = 15
	slotDWCreateTextLayout           = 18
	slotDWCreateEllipsisTrimmingSign = 20

	slotTFSetTextAlignment      = 3
	slotTFSetParagraphAlignment = 4
	slotTFSetWordWrapping       = 5
	slotTFSetTrimming           = 9
	slotTFSetLineSpacing        = 10
	slotTLGetMetrics            = 60
	slotTLHitTestTextPosition   = 65
	slotTLHitTestTextRange      = 66

	slotWICCreateBitmap           = 17
	slotWICCreateBitmapFromMemory = 20
	slotWICBitmapCopyPixels       = 7
)

const (
	dxgiFormatB8G8R8A8         = 87
	d2dAlphaPremultiplied      = 1
	d2dTargetSoftware          = 1
	d2dTextAntialiasDefault    = 0
	d2dTextAntialiasGray       = 2
	d2dAntialiasPerPrimitive   = 0
	d2dInterpolationNearest    = 0
	d2dInterpolationLinear     = 1
	d2dDrawTextEnableColorFont = 4
	d2dDrawTextClip            = 2
	d2dErrRecreateTarget       = 0x8899000C
	d2dFigureBeginFilled       = 0
	d2dFigureBeginHollow       = 1
	d2dFigureEndOpen           = 0
	d2dFigureEndClosed         = 1
	d2dCapRound                = 2
	d2dJoinRound               = 2
	d2dDashSolid               = 0
	d2dDashDash                = 1

	dwriteStretchNormal      = 5
	dwriteNoWrap             = 1
	dwriteWrap               = 0
	dwriteTrimCharacter      = 1
	dwriteLineSpacingUniform = 1
	wicBitmapCacheOnLoad     = 2
)

type d2dColor struct{ R, G, B, A float32 }

type d2dRect struct{ Left, Top, Right, Bottom float32 }

type d2dRoundedRect struct {
	Rect             d2dRect
	RadiusX, RadiusY float32
}

type d2dEllipse struct {
	X, Y, RadiusX, RadiusY float32
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

type d2dStrokeStyleProperties struct {
	StartCap, EndCap, DashCap, LineJoin uint32
	MiterLimit                          float32
	DashStyle                           uint32
	DashOffset                          float32
}

var (
	d2dFactory  comObject
	dwFactory   comObject
	wicFactory  comObject
	roundStroke comObject
	dashStroke  comObject
)

// factories creates the Direct2D, DirectWrite and WIC factories once, on
// the UI thread.
func factories() error {
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
	if hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidWICImagingFactory)), 0, 1,
		uintptr(unsafe.Pointer(&iidIWICImagingFactory)), uintptr(unsafe.Pointer(&wic))); !succeeded(hr) {
		release(f)
		release(dw)
		return errors.New("创建 WIC 失败")
	}
	d2dFactory, dwFactory, wicFactory = f, dw, wic
	round := d2dStrokeStyleProperties{StartCap: d2dCapRound, EndCap: d2dCapRound, DashCap: d2dCapRound, LineJoin: d2dJoinRound, MiterLimit: 10}
	call(d2dFactory, slotFactoryCreateStrokeStyle, uintptr(unsafe.Pointer(&round)), 0, 0, uintptr(unsafe.Pointer(&roundStroke)))
	dash := d2dStrokeStyleProperties{MiterLimit: 10, DashStyle: d2dDashDash}
	call(d2dFactory, slotFactoryCreateStrokeStyle, uintptr(unsafe.Pointer(&dash)), 0, 0, uintptr(unsafe.Pointer(&dashStroke)))
	return nil
}

func f32(v float64) uintptr { return uintptr(math.Float32bits(float32(v))) }

// point2F packs a D2D1_POINT_2F, which is passed by value in one register.
func point2F(x, y float64) uintptr {
	return uintptr(math.Float32bits(float32(x))) | uintptr(math.Float32bits(float32(y)))<<32
}
