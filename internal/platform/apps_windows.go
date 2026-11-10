package platform

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32                         = windows.NewLazySystemDLL("shell32.dll")
	procSHCreateItemFromParsingName = shell32.NewProc("SHCreateItemFromParsingName")
	procGetObjectW                  = gdi32.NewProc("GetObjectW")
	procGetDIBits                   = gdi32.NewProc("GetDIBits")
)

var iidIShellItemImageFactory = windows.GUID{Data1: 0xbcc18b79, Data2: 0xba16, Data3: 0x442f, Data4: [8]byte{0x80, 0xc4, 0x8a, 0x59, 0xc3, 0x0c, 0x46, 0x3b}}

const (
	slotGetImage          = 3 // IShellItemImageFactory
	siigbfBiggerSizeOK    = 0x1
	siigbfIconOnly        = 0x4
	coinitApartment       = 0x2
	createNoWindow        = 0x08000000
	appsFolder            = `shell:AppsFolder\`
	dibRGBColors          = 0
	showNormal            = 1
	powershellStartApps   = `[Console]::OutputEncoding = [Text.Encoding]::UTF8; ConvertTo-Json -Compress -InputObject @(Get-StartApps | Select-Object Name, AppID)`
	maxIconPixels         = 256
	bitmapInfoHeaderBytes = 40
)

// Applications lists the apps of the Start menu, Store apps included. On
// Windows an app's ID is its AppsFolder ID.
func Applications() []App {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", powershellStartApps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var entries []struct {
		Name  string
		AppID string
	}
	if json.Unmarshal(bytes.TrimPrefix(out, []byte("\xef\xbb\xbf")), &entries) != nil {
		return nil
	}
	apps := make([]App, 0, len(entries))
	for _, e := range entries {
		if e.Name == "" || e.AppID == "" || isUninstaller(e.Name) {
			continue
		}
		apps = append(apps, App{ID: e.AppID, Name: e.Name})
	}
	return apps
}

// isUninstaller reports whether a Start menu entry only removes a program.
func isUninstaller(name string) bool {
	n := strings.ToLower(name)
	return strings.HasPrefix(n, "uninstall") || strings.HasPrefix(n, "卸载") || strings.Contains(n, " uninstall")
}

// Icon returns the icon of an app (by ID) or a file, as a PNG of the given
// size in DIPs, at twice the pixels for high-DPI screens.
func Icon(id string, size int) []byte {
	target := id
	if !strings.Contains(id, `:\`) && !strings.HasPrefix(id, `\\`) {
		target = appsFolder + id
	}
	img := shellImage(target, min(size*2, maxIconPixels), siigbfIconOnly|siigbfBiggerSizeOK)
	if img.Width == 0 {
		return nil
	}
	nrgba := image.NewNRGBA(image.Rect(0, 0, img.Width, img.Height))
	for i := 0; i < len(img.Pixels); i += 4 {
		b, g, r, a := img.Pixels[i], img.Pixels[i+1], img.Pixels[i+2], img.Pixels[i+3]
		if a != 0 && a != 255 {
			r, g, b = unpremultiply(r, a), unpremultiply(g, a), unpremultiply(b, a)
		}
		nrgba.Pix[i], nrgba.Pix[i+1], nrgba.Pix[i+2], nrgba.Pix[i+3] = r, g, b, a
	}
	var buf bytes.Buffer
	if png.Encode(&buf, nrgba) != nil {
		return nil
	}
	return buf.Bytes()
}

// ItemImage returns what File Explorer shows for a shell item, given by its
// parsing name: a thumbnail where the item has one, its icon otherwise, at
// most pixels wide and high. The pixels are BGRA with premultiplied alpha;
// an empty Image means there is none.
func ItemImage(parsingName string, pixels int) Image {
	img := shellImage(parsingName, pixels, 0)
	// Some thumbnails come without alpha; they are opaque.
	opaque := true
	for i := 3; i < len(img.Pixels); i += 4 {
		if img.Pixels[i] != 0 {
			opaque = false
			break
		}
	}
	if opaque {
		for i := 3; i < len(img.Pixels); i += 4 {
			img.Pixels[i] = 255
		}
	}
	return img
}

// shellImage asks a shell item for its image with IShellItemImageFactory.
func shellImage(target string, pixels int, flags uintptr) Image {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, coinitApartment); err == nil {
		defer windows.CoUninitialize()
	}
	name, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return Image{}
	}
	var factory comObject
	hr, _, _ := procSHCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(name)), 0,
		uintptr(unsafe.Pointer(&iidIShellItemImageFactory)), uintptr(unsafe.Pointer(&factory)))
	if !succeeded(hr) || factory == nil {
		return Image{}
	}
	defer release(factory)
	px := uintptr(pixels)
	var bitmap uintptr
	// SIZE is passed by value; its two 32-bit fields fit in one register.
	if !succeeded(call(factory, slotGetImage, px|px<<32, flags, uintptr(unsafe.Pointer(&bitmap)))) || bitmap == 0 {
		return Image{}
	}
	defer procDeleteObject.Call(bitmap)
	return bitmapPixels(bitmap)
}

// bitmapPixels reads a 32-bit HBITMAP as top-down BGRA.
func bitmapPixels(bitmap uintptr) Image {
	var bm struct {
		Type, Width, Height, WidthBytes int32
		Planes, BitsPixel               uint16
		Bits                            uintptr
	}
	if n, _, _ := procGetObjectW.Call(bitmap, unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm))); n == 0 || bm.Width <= 0 || bm.Height <= 0 {
		return Image{}
	}
	screen, _, _ := procGetDC.Call(0)
	if screen == 0 {
		return Image{}
	}
	defer procReleaseDC.Call(0, screen)
	width, height := int(bm.Width), int(bm.Height)
	info := bitmapInfo{Size: bitmapInfoHeaderBytes, Width: bm.Width, Height: -bm.Height, Planes: 1, BitCount: 32}
	bgra := make([]byte, width*height*4)
	if n, _, _ := procGetDIBits.Call(screen, bitmap, 0, uintptr(height), uintptr(unsafe.Pointer(&bgra[0])), uintptr(unsafe.Pointer(&info)), dibRGBColors); n == 0 {
		return Image{}
	}
	return Image{Pixels: bgra, Width: width, Height: height}
}

func unpremultiply(c, a byte) byte {
	return byte(min(int(c)*255/int(a), 255))
}

// OpenApp starts an app, or brings it to the front.
func OpenApp(id string) error {
	return shellOpen(appsFolder + id)
}

// OpenFile opens a file or folder with its default program.
func OpenFile(path string) error {
	return shellOpen(path)
}

func shellOpen(target string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	if err := windows.ShellExecute(0, verb, file, nil, nil, showNormal); err != nil {
		return errors.New("无法打开 " + target)
	}
	return nil
}

// RevealFile shows a file in File Explorer.
func RevealFile(path string) {
	cmd := exec.Command("explorer.exe")
	// explorer parses /select itself and wants the path quoted after it.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + path + `"`}
	_ = cmd.Start()
}
