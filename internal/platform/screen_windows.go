package platform

import (
	"errors"
	"runtime"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

var (
	combase                       = windows.NewLazySystemDLL("combase.dll")
	procRoInitialize              = combase.NewProc("RoInitialize")
	procRoUninitialize            = combase.NewProc("RoUninitialize")
	procRoGetActivationFactory    = combase.NewProc("RoGetActivationFactory")
	procWindowsCreateString       = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString       = combase.NewProc("WindowsDeleteString")
	procWindowsGetStringRawBuffer = combase.NewProc("WindowsGetStringRawBuffer")

	gdi32            = windows.NewLazySystemDLL("gdi32.dll")
	procDeleteObject = gdi32.NewProc("DeleteObject")
	procGetDC        = user32.NewProc("GetDC")
	procReleaseDC    = user32.NewProc("ReleaseDC")

	dwmapi       = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmFlush = dwmapi.NewProc("DwmFlush")
)

var (
	iidIOcrEngineStatics           = windows.GUID{Data1: 0x5bffa85a, Data2: 0x3384, Data3: 0x3540, Data4: [8]byte{0x99, 0x40, 0x69, 0x91, 0x20, 0xd4, 0x28, 0xa8}}
	iidISoftwareBitmapStatics      = windows.GUID{Data1: 0xdf0385db, Data2: 0x672f, Data3: 0x4a9d, Data4: [8]byte{0x80, 0x6e, 0xc2, 0x44, 0x2f, 0x34, 0x3e, 0x86}}
	iidICryptographicBufferStatics = windows.GUID{Data1: 0x320b7e22, Data2: 0x3cb0, Data3: 0x4cdf, Data4: [8]byte{0x86, 0x63, 0x1d, 0x28, 0x91, 0x00, 0x65, 0xeb}}
	iidIAsyncInfo                  = windows.GUID{Data1: 0x00000036, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
)

// Method slots in the WinRT interfaces' virtual tables. Slots 0 to 5 are
// IUnknown's and IInspectable's.
const (
	slotQueryInterface                    = 0
	slotTryCreateFromUserProfileLanguages = 10 // IOcrEngineStatics
	slotRecognizeAsync                    = 6  // IOcrEngine
	slotCreateFromByteArray               = 9  // ICryptographicBufferStatics
	slotCreateCopyWithAlphaFromBuffer     = 10 // ISoftwareBitmapStatics
	slotAsyncStatus                       = 7  // IAsyncInfo
	slotAsyncGetResults                   = 8  // IAsyncOperation<T>
	slotResultLines                       = 6  // IOcrResult
	slotVectorGetAt                       = 6  // IVectorView<T>
	slotVectorSize                        = 7  // IVectorView<T>
	slotLineWords                         = 6  // IOcrLine
	slotLineText                          = 7  // IOcrLine
	slotWordBoundingRect                  = 6  // IOcrWord
)

const (
	roInitMultithreaded = 1
	pixelFormatBgra8    = 87
	alphaModeIgnore     = 2
	asyncStarted        = 0
	asyncCompleted      = 1
)

// ScreenCaptureAllowed reports whether arubox may read the screen. Windows
// has no such permission.
func ScreenCaptureAllowed() bool { return true }

// RequestScreenCapture does nothing on Windows.
func RequestScreenCapture() {}

// OpenScreenCaptureSettings does nothing on Windows.
func OpenScreenCaptureSettings() {}

// CaptureScreen takes a picture of r at the screen's full resolution.
func CaptureScreen(r application.Rect) (Image, error) {
	// Let the screen drop windows that were just hidden before taking it.
	procDwmFlush.Call()
	p := application.DipToPhysicalRect(r)
	if p.Width <= 0 || p.Height <= 0 {
		return Image{}, errors.New("截取屏幕失败")
	}
	pixels, err := capturePixels(p)
	if err != nil {
		return Image{}, err
	}
	return Image{Pixels: pixels, Width: p.Width, Height: p.Height}, nil
}

// ImageText reads the lines of text in a picture with the system's OCR.
// Line boxes are in its pixels.
func ImageText(img Image) ([]TextLine, error) {
	if img.Width <= 0 || img.Height <= 0 || len(img.Pixels) < img.Width*img.Height*4 {
		return nil, nil
	}
	return recognize(img.Pixels, img.Width, img.Height)
}

type bitmapInfo struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
	Colors        [1]uint32
}

// recognize runs Windows.Media.Ocr on BGRA pixels, in the languages of the
// user's profile.
func recognize(pixels []byte, width, height int) ([]TextLine, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if hr, _, _ := procRoInitialize.Call(roInitMultithreaded); succeeded(hr) {
		defer procRoUninitialize.Call()
	}
	failed := errors.New("识别文字失败")

	engines, err := activationFactory("Windows.Media.Ocr.OcrEngine", &iidIOcrEngineStatics)
	if err != nil {
		return nil, failed
	}
	defer release(engines)
	var engine comObject
	if !succeeded(call(engines, slotTryCreateFromUserProfileLanguages, uintptr(unsafe.Pointer(&engine)))) || engine == nil {
		return nil, errors.New("系统没有可用的文字识别语言，请在 Windows 设置里为语言安装“光学字符识别”功能")
	}
	defer release(engine)

	bitmap, err := softwareBitmap(pixels, width, height)
	if err != nil {
		return nil, failed
	}
	defer release(bitmap)

	var operation comObject
	if !succeeded(call(engine, slotRecognizeAsync, uintptr(bitmap), uintptr(unsafe.Pointer(&operation)))) || operation == nil {
		return nil, failed
	}
	defer release(operation)
	if !await(operation) {
		return nil, failed
	}
	var result comObject
	if !succeeded(call(operation, slotAsyncGetResults, uintptr(unsafe.Pointer(&result)))) || result == nil {
		return nil, failed
	}
	defer release(result)
	return ocrLines(result), nil
}

// softwareBitmap wraps BGRA pixels in a SoftwareBitmap, through an IBuffer
// that CryptographicBuffer makes from the bytes.
func softwareBitmap(pixels []byte, width, height int) (comObject, error) {
	buffers, err := activationFactory("Windows.Security.Cryptography.CryptographicBuffer", &iidICryptographicBufferStatics)
	if err != nil {
		return nil, err
	}
	defer release(buffers)
	var buffer comObject
	if !succeeded(call(buffers, slotCreateFromByteArray, uintptr(len(pixels)), uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&buffer)))) || buffer == nil {
		return nil, errors.New("create buffer")
	}
	defer release(buffer)

	bitmaps, err := activationFactory("Windows.Graphics.Imaging.SoftwareBitmap", &iidISoftwareBitmapStatics)
	if err != nil {
		return nil, err
	}
	defer release(bitmaps)
	var bitmap comObject
	// The screen's alpha bytes are meaningless, so they are ignored.
	if !succeeded(call(bitmaps, slotCreateCopyWithAlphaFromBuffer, uintptr(buffer), pixelFormatBgra8,
		uintptr(width), uintptr(height), alphaModeIgnore, uintptr(unsafe.Pointer(&bitmap)))) || bitmap == nil {
		return nil, errors.New("create bitmap")
	}
	return bitmap, nil
}

// await waits for a WinRT async operation and reports whether it completed.
func await(operation comObject) bool {
	var info comObject
	if !succeeded(call(operation, slotQueryInterface, uintptr(unsafe.Pointer(&iidIAsyncInfo)), uintptr(unsafe.Pointer(&info)))) || info == nil {
		return false
	}
	defer release(info)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var status int32
		if !succeeded(call(info, slotAsyncStatus, uintptr(unsafe.Pointer(&status)))) {
			return false
		}
		if status != asyncStarted {
			return status == asyncCompleted
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// ocrLines reads an OcrResult's lines, boxing each around its words.
func ocrLines(result comObject) []TextLine {
	var lines comObject
	if !succeeded(call(result, slotResultLines, uintptr(unsafe.Pointer(&lines)))) || lines == nil {
		return nil
	}
	defer release(lines)
	var count uint32
	call(lines, slotVectorSize, uintptr(unsafe.Pointer(&count)))
	out := make([]TextLine, 0, count)
	for i := range count {
		var line comObject
		if !succeeded(call(lines, slotVectorGetAt, uintptr(i), uintptr(unsafe.Pointer(&line)))) || line == nil {
			continue
		}
		var text hstring
		call(line, slotLineText, uintptr(unsafe.Pointer(&text)))
		out = append(out, TextLine{Text: text.String(), Box: wordsBox(line)})
		text.Delete()
		release(line)
	}
	return out
}

// wordsBox returns the rectangle around a line's words.
func wordsBox(line comObject) Box {
	var words comObject
	if !succeeded(call(line, slotLineWords, uintptr(unsafe.Pointer(&words)))) || words == nil {
		return Box{}
	}
	defer release(words)
	var count uint32
	call(words, slotVectorSize, uintptr(unsafe.Pointer(&count)))
	var box Box
	for i := range count {
		var word comObject
		if !succeeded(call(words, slotVectorGetAt, uintptr(i), uintptr(unsafe.Pointer(&word)))) || word == nil {
			continue
		}
		var rect struct{ X, Y, Width, Height float32 }
		call(word, slotWordBoundingRect, uintptr(unsafe.Pointer(&rect)))
		release(word)
		b := Box{X: float64(rect.X), Y: float64(rect.Y), Width: float64(rect.Width), Height: float64(rect.Height)}
		if i == 0 {
			box = b
			continue
		}
		right := max(box.X+box.Width, b.X+b.Width)
		bottom := max(box.Y+box.Height, b.Y+b.Height)
		box.X, box.Y = min(box.X, b.X), min(box.Y, b.Y)
		box.Width, box.Height = right-box.X, bottom-box.Y
	}
	return box
}

// activationFactory returns a WinRT class's factory, as the interface iid.
func activationFactory(class string, iid *windows.GUID) (comObject, error) {
	name, err := newHString(class)
	if err != nil {
		return nil, err
	}
	defer name.Delete()
	var factory comObject
	hr, _, _ := procRoGetActivationFactory.Call(uintptr(name), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&factory)))
	if !succeeded(hr) || factory == nil {
		return nil, errors.New("activate " + class)
	}
	return factory, nil
}

// hstring is a WinRT string handle.
type hstring uintptr

func newHString(s string) (hstring, error) {
	utf16, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h hstring
	hr, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&utf16[0])), uintptr(len(utf16)-1), uintptr(unsafe.Pointer(&h)))
	if !succeeded(hr) {
		return 0, errors.New("create string")
	}
	return h, nil
}

func (h hstring) String() string {
	if h == 0 {
		return ""
	}
	var length uint32
	p, _, _ := procWindowsGetStringRawBuffer.Call(uintptr(h), uintptr(unsafe.Pointer(&length)))
	if p == 0 {
		return ""
	}
	// The buffer belongs to the HSTRING, outside Go's heap.
	return windows.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(p)), length)) //nolint:govet
}

func (h hstring) Delete() {
	if h != 0 {
		procWindowsDeleteString.Call(uintptr(h))
	}
}
