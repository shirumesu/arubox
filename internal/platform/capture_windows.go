package platform

import (
	"errors"
	"math"
	"runtime"
	"sync"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

// The screen is read through DXGI desktop duplication rather than GDI. With
// HDR on, the desktop is composed in floating point, and GDI's 8-bit copy
// of it washes out windows that draw in HDR, such as Edge while a page plays
// HDR video: its light greys and borders come out white. Duplication hands
// over the composed desktop as it is, which is converted here the way the
// screen shows SDR content.

var (
	dxgi                            = windows.NewLazySystemDLL("dxgi.dll")
	d3d11                           = windows.NewLazySystemDLL("d3d11.dll")
	procCreateDXGIFactory1          = dxgi.NewProc("CreateDXGIFactory1")
	procD3D11CreateDevice           = d3d11.NewProc("D3D11CreateDevice")
	procGetDisplayConfigBufferSizes = user32.NewProc("GetDisplayConfigBufferSizes")
	procQueryDisplayConfig          = user32.NewProc("QueryDisplayConfig")
	procDisplayConfigGetDeviceInfo  = user32.NewProc("DisplayConfigGetDeviceInfo")
	iidIDXGIFactory1                = windows.GUID{Data1: 0x770aae78, Data2: 0xf26f, Data3: 0x4dba, Data4: [8]byte{0xa8, 0x29, 0x25, 0x3c, 0x83, 0xd1, 0xb3, 0x87}}
	iidIDXGIOutput5                 = windows.GUID{Data1: 0x80a07424, Data2: 0xab52, Data3: 0x42eb, Data4: [8]byte{0x83, 0x3c, 0x0c, 0x42, 0xfd, 0x28, 0x2d, 0x98}}
	iidID3D11Texture2D              = windows.GUID{Data1: 0x6f15aaf2, Data2: 0xd208, Data3: 0x4e89, Data4: [8]byte{0x9a, 0xb4, 0x48, 0x95, 0x35, 0xd3, 0x4f, 0x9c}}
	errDisplayConfigNotFound        = errors.New("display not found")
)

// Method slots in the COM interfaces' virtual tables (dxgi1_5.h, d3d11.h).
const (
	slotEnumAdapters1         = 12 // IDXGIFactory1
	slotEnumOutputs           = 7  // IDXGIAdapter
	slotAdapterGetDesc        = 8  // IDXGIAdapter
	slotOutputGetDesc         = 7  // IDXGIOutput
	slotDuplicateOutput1      = 26 // IDXGIOutput5
	slotAcquireNextFrame      = 8  // IDXGIOutputDuplication
	slotReleaseFrame          = 14 // IDXGIOutputDuplication
	slotTextureGetDesc        = 10 // ID3D11Texture2D
	slotCreateTexture2D       = 5  // ID3D11Device
	slotContextMap            = 14 // ID3D11DeviceContext
	slotContextUnmap          = 15 // ID3D11DeviceContext
	slotContextCopyResource   = 47 // ID3D11DeviceContext
	d3d11SDKVersion           = 7
	formatR16G16B16A16Float   = 10
	formatB8G8R8A8Unorm       = 87
	usageStaging              = 3
	cpuAccessRead             = 0x20000
	mapRead                   = 1
	rotate90                  = 2
	rotate180                 = 3
	rotate270                 = 4
	frameWaitMs               = 500
	qdcOnlyActivePaths        = 2
	displayConfigSourceName   = 1
	displayConfigSDRWhite     = 11
	sizeofPathInfo            = 72
	sizeofModeInfo            = 64
	sizeofSourceDeviceName    = 84
	sizeofSDRWhiteLevel       = 24
	sizeofAdapterDesc         = 304         // DXGI_ADAPTER_DESC
	adapterDescLUID           = 296         // its AdapterLuid
	sdrWhiteLevelUnitsPerNits = 1000.0 / 80 // SDRWhiteLevel is 1000 for 80 nits
	scRGBNits                 = 80.0        // scRGB 1.0 is 80 nits
)

type outputDesc struct {
	DeviceName               [32]uint16
	Left, Top, Right, Bottom int32
	AttachedToDesktop        int32
	Rotation                 uint32
	Monitor                  uintptr
}

type texture2DDesc struct {
	Width, Height, MipLevels, ArraySize, Format uint32
	SampleCount, SampleQuality                  uint32
	Usage, BindFlags, CPUAccessFlags, MiscFlags uint32
}

type mappedSubresource struct {
	Data                 unsafe.Pointer
	RowPitch, DepthPitch uint32
}

// captureDevice is the Direct3D device frames are read with, kept between
// captures since making one takes about 200 ms. It belongs to the adapter
// whose LUID is luid, and mu serializes captures.
var captureDevice struct {
	mu      sync.Mutex
	luid    uint64
	device  comObject
	context comObject
}

// PrepareScreenCapture makes the device for reading the primary screen, so
// the first capture is as quick as the next.
func PrepareScreenCapture() {
	var factory comObject
	if hr, _, _ := procCreateDXGIFactory1.Call(uintptr(unsafe.Pointer(&iidIDXGIFactory1)), uintptr(unsafe.Pointer(&factory))); !succeeded(hr) || factory == nil {
		return
	}
	defer release(factory)
	adapter, output, _ := findOutput(factory, application.Rect{Width: 1, Height: 1})
	if output == nil {
		return
	}
	defer release(adapter)
	defer release(output)
	captureDevice.mu.Lock()
	defer captureDevice.mu.Unlock()
	if captureDevice.device == nil || captureDevice.luid != adapterLUID(adapter) {
		makeCaptureDevice(adapter)
	}
}

// capturePixels copies a rectangle of one screen, in physical pixels, as
// top-down BGRA.
func capturePixels(r application.Rect) ([]byte, error) {
	failed := errors.New("截取屏幕失败")
	var factory comObject
	if hr, _, _ := procCreateDXGIFactory1.Call(uintptr(unsafe.Pointer(&iidIDXGIFactory1)), uintptr(unsafe.Pointer(&factory))); !succeeded(hr) || factory == nil {
		return nil, failed
	}
	defer release(factory)
	adapter, output, desc := findOutput(factory, r)
	if output == nil {
		return nil, failed
	}
	defer release(adapter)
	defer release(output)
	var output5 comObject
	if !succeeded(call(output, slotQueryInterface, uintptr(unsafe.Pointer(&iidIDXGIOutput5)), uintptr(unsafe.Pointer(&output5)))) || output5 == nil {
		return nil, failed
	}
	defer release(output5)

	captureDevice.mu.Lock()
	defer captureDevice.mu.Unlock()
	duplication := duplicate(adapter, output5)
	if duplication == nil {
		return nil, failed
	}
	defer release(duplication)
	device, context := captureDevice.device, captureDevice.context

	// The first frames may carry only the pointer, without a picture.
	var frameInfo [64]byte
	var resource comObject
	for {
		if !succeeded(call(duplication, slotAcquireNextFrame, frameWaitMs,
			uintptr(unsafe.Pointer(&frameInfo)), uintptr(unsafe.Pointer(&resource)))) || resource == nil {
			return nil, failed
		}
		if lastPresent := *(*int64)(unsafe.Pointer(&frameInfo[0])); lastPresent != 0 {
			break
		}
		release(resource)
		call(duplication, slotReleaseFrame)
	}
	defer call(duplication, slotReleaseFrame)
	defer release(resource)
	var frame comObject
	if !succeeded(call(resource, slotQueryInterface, uintptr(unsafe.Pointer(&iidID3D11Texture2D)), uintptr(unsafe.Pointer(&frame)))) || frame == nil {
		return nil, failed
	}
	defer release(frame)

	// The frame stays on the GPU; a staging copy can be read.
	var td texture2DDesc
	call(frame, slotTextureGetDesc, uintptr(unsafe.Pointer(&td)))
	sd := texture2DDesc{Width: td.Width, Height: td.Height, MipLevels: 1, ArraySize: 1, Format: td.Format,
		SampleCount: 1, Usage: usageStaging, CPUAccessFlags: cpuAccessRead}
	var staging comObject
	if !succeeded(call(device, slotCreateTexture2D, uintptr(unsafe.Pointer(&sd)), 0, uintptr(unsafe.Pointer(&staging)))) || staging == nil {
		return nil, failed
	}
	defer release(staging)
	call(context, slotContextCopyResource, uintptr(staging), uintptr(frame))
	var mapped mappedSubresource
	if !succeeded(call(context, slotContextMap, uintptr(staging), 0, mapRead, 0, uintptr(unsafe.Pointer(&mapped)))) || mapped.Data == nil {
		return nil, failed
	}
	defer call(context, slotContextUnmap, uintptr(staging), 0)

	src := unsafe.Slice((*byte)(mapped.Data), int(mapped.RowPitch)*int(td.Height))
	var toSRGB *[65536]uint8
	switch td.Format {
	case formatB8G8R8A8Unorm:
	case formatR16G16B16A16Float:
		white, err := sdrWhiteLevel(desc.DeviceName)
		if err != nil {
			return nil, failed
		}
		toSRGB = halfToSRGB(white)
	default:
		return nil, failed
	}
	return desktopPixels(src, int(mapped.RowPitch), toSRGB, desc, r), nil
}

// duplicate starts duplicating output with the kept device, making a new one
// when there is none for adapter or the kept one was lost, as it is when
// the graphics driver restarts. captureDevice.mu must be held.
func duplicate(adapter, output5 comObject) comObject {
	luid := adapterLUID(adapter)
	if captureDevice.device != nil && captureDevice.luid == luid {
		if d := duplicateWith(captureDevice.device, output5); d != nil {
			return d
		}
	}
	if !makeCaptureDevice(adapter) {
		return nil
	}
	return duplicateWith(captureDevice.device, output5)
}

// makeCaptureDevice replaces the kept device with a new one for adapter.
// captureDevice.mu must be held.
func makeCaptureDevice(adapter comObject) bool {
	release(captureDevice.context)
	release(captureDevice.device)
	captureDevice.device, captureDevice.context = nil, nil
	var device, context comObject
	hr, _, _ := procD3D11CreateDevice.Call(uintptr(adapter), 0, 0, 0, 0, 0, d3d11SDKVersion,
		uintptr(unsafe.Pointer(&device)), 0, uintptr(unsafe.Pointer(&context)))
	if !succeeded(hr) || device == nil || context == nil {
		release(context)
		release(device)
		return false
	}
	captureDevice.luid, captureDevice.device, captureDevice.context = adapterLUID(adapter), device, context
	return true
}

func duplicateWith(device, output5 comObject) comObject {
	formats := [...]uint32{formatR16G16B16A16Float, formatB8G8R8A8Unorm}
	var duplication comObject
	if !succeeded(call(output5, slotDuplicateOutput1, uintptr(device), 0, uintptr(len(formats)),
		uintptr(unsafe.Pointer(&formats[0])), uintptr(unsafe.Pointer(&duplication)))) {
		return nil
	}
	return duplication
}

func adapterLUID(adapter comObject) uint64 {
	var desc [sizeofAdapterDesc]byte
	call(adapter, slotAdapterGetDesc, uintptr(unsafe.Pointer(&desc)))
	return *(*uint64)(unsafe.Pointer(&desc[adapterDescLUID]))
}

// findOutput returns the screen holding the middle of r, with its adapter.
func findOutput(factory comObject, r application.Rect) (adapter, output comObject, desc outputDesc) {
	cx, cy := int32(r.X+r.Width/2), int32(r.Y+r.Height/2)
	for i := uintptr(0); ; i++ {
		var a comObject
		if !succeeded(call(factory, slotEnumAdapters1, i, uintptr(unsafe.Pointer(&a)))) || a == nil {
			return nil, nil, outputDesc{}
		}
		for j := uintptr(0); ; j++ {
			var o comObject
			if !succeeded(call(a, slotEnumOutputs, j, uintptr(unsafe.Pointer(&o)))) || o == nil {
				break
			}
			var d outputDesc
			call(o, slotOutputGetDesc, uintptr(unsafe.Pointer(&d)))
			if d.AttachedToDesktop != 0 && cx >= d.Left && cx < d.Right && cy >= d.Top && cy < d.Bottom {
				return a, o, d
			}
			release(o)
		}
		release(a)
	}
}

// desktopPixels copies r out of a duplicated frame as BGRA. The frame is in
// the screen's own orientation, which a rotated screen turns. toSRGB maps
// half floats to sRGB bytes for a floating-point frame, nil for a BGRA one.
func desktopPixels(src []byte, pitch int, toSRGB *[65536]uint8, d outputDesc, r application.Rect) []byte {
	out := make([]byte, r.Width*r.Height*4)
	// The rows are converted on every core: one takes about 45 ms for a 4K
	// screen.
	var wg sync.WaitGroup
	chunk := (r.Height + runtime.NumCPU() - 1) / runtime.NumCPU()
	for y0 := 0; y0 < r.Height; y0 += chunk {
		wg.Go(func() {
			convertRows(out, src, pitch, toSRGB, d, r, y0, min(y0+chunk, r.Height))
		})
	}
	wg.Wait()
	return out
}

// convertRows fills rows y0 to y1 of r in out, for desktopPixels.
func convertRows(out, src []byte, pitch int, toSRGB *[65536]uint8, d outputDesc, r application.Rect, y0, y1 int) {
	w, h := int(d.Right-d.Left), int(d.Bottom-d.Top)
	for y := y0; y < y1; y++ {
		dy := r.Y + y - int(d.Top)
		for x := range r.Width {
			dx := r.X + x - int(d.Left)
			if dx < 0 || dy < 0 || dx >= w || dy >= h {
				continue
			}
			tx, ty := dx, dy
			switch d.Rotation {
			case rotate90:
				tx, ty = h-1-dy, dx
			case rotate180:
				tx, ty = w-1-dx, h-1-dy
			case rotate270:
				tx, ty = dy, w-1-dx
			}
			o := (y*r.Width + x) * 4
			if toSRGB == nil {
				i := ty*pitch + tx*4
				copy(out[o:o+4], src[i:i+4])
				continue
			}
			i := ty*pitch + tx*8
			half := func(k int) uint16 { return uint16(src[i+k]) | uint16(src[i+k+1])<<8 }
			out[o], out[o+1], out[o+2], out[o+3] = toSRGB[half(4)], toSRGB[half(2)], toSRGB[half(0)], 255
		}
	}
}

// halfToSRGB maps every half float of scRGB, the linear light the desktop
// is composed in, to the sRGB byte the screen shows it as: SDR white sits at
// the white level the user set, and brighter light clips to white.
func halfToSRGB(whiteNits float64) *[65536]uint8 {
	var table [65536]uint8
	scale := scRGBNits / whiteNits
	for h := range table {
		v := float64(halfFloat(uint16(h))) * scale
		switch {
		case math.IsNaN(v) || v <= 0:
			table[h] = 0
		case v >= 1:
			table[h] = 255
		case v <= 0.0031308:
			table[h] = uint8(v*12.92*255 + 0.5)
		default:
			table[h] = uint8((1.055*math.Pow(v, 1/2.4)-0.055)*255 + 0.5)
		}
	}
	return &table
}

func halfFloat(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := uint32(h>>10) & 0x1f
	frac := uint32(h) & 0x3ff
	switch {
	case exp == 0 && frac == 0:
		return math.Float32frombits(sign)
	case exp == 0: // subnormal
		return float32(frac) / (1 << 24) * float32(1-2*int(h>>15))
	case exp == 0x1f:
		return math.Float32frombits(sign | 0x7f800000 | frac<<13)
	}
	return math.Float32frombits(sign | (exp+112)<<23 | frac<<13)
}

// sdrWhiteLevel returns the brightness, in nits, that the screen with the
// given GDI device name shows SDR white at.
func sdrWhiteLevel(device [32]uint16) (float64, error) {
	var paths, modes uint32
	if r, _, _ := procGetDisplayConfigBufferSizes.Call(qdcOnlyActivePaths, uintptr(unsafe.Pointer(&paths)), uintptr(unsafe.Pointer(&modes))); r != 0 {
		return 0, errDisplayConfigNotFound
	}
	pathInfo := make([]byte, sizeofPathInfo*max(paths, 1))
	modeInfo := make([]byte, sizeofModeInfo*max(modes, 1))
	if r, _, _ := procQueryDisplayConfig.Call(qdcOnlyActivePaths, uintptr(unsafe.Pointer(&paths)), uintptr(unsafe.Pointer(&pathInfo[0])),
		uintptr(unsafe.Pointer(&modes)), uintptr(unsafe.Pointer(&modeInfo[0])), 0); r != 0 {
		return 0, errDisplayConfigNotFound
	}
	for i := range int(paths) {
		path := pathInfo[i*sizeofPathInfo:]
		// The source (at 0) is the desktop the GDI name belongs to; the
		// target (at 20) the monitor whose white level is asked for. Each
		// starts with an adapter LUID and an ID, as a request header does.
		name := make([]byte, sizeofSourceDeviceName)
		if !displayConfigInfo(name, displayConfigSourceName, path[0:12]) {
			continue
		}
		if [32]uint16(unsafe.Slice((*uint16)(unsafe.Pointer(&name[20])), 32)) != device {
			continue
		}
		level := make([]byte, sizeofSDRWhiteLevel)
		if !displayConfigInfo(level, displayConfigSDRWhite, path[20:32]) {
			return 0, errDisplayConfigNotFound
		}
		return float64(*(*uint32)(unsafe.Pointer(&level[20]))) / sdrWhiteLevelUnitsPerNits, nil
	}
	return 0, errDisplayConfigNotFound
}

// displayConfigInfo fills buf, which starts with a request header, with the
// device information of type for the adapter and ID in id.
func displayConfigInfo(buf []byte, typ uint32, id []byte) bool {
	*(*uint32)(unsafe.Pointer(&buf[0])) = typ
	*(*uint32)(unsafe.Pointer(&buf[4])) = uint32(len(buf))
	copy(buf[8:20], id)
	r, _, _ := procDisplayConfigGetDeviceInfo.Call(uintptr(unsafe.Pointer(&buf[0])))
	return r == 0
}
