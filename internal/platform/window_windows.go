package platform

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var user32 = windows.NewLazySystemDLL("user32.dll")

var (
	procEnumWindows              = user32.NewProc("EnumWindows")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procIsIconic                 = user32.NewProc("IsIconic")
	procGetWindowLongPtrW        = user32.NewProc("GetWindowLongPtrW")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procDwmGetWindowAttribute    = dwmapi.NewProc("DwmGetWindowAttribute")
)

const (
	gwlExStyle               = ^uintptr(19) // GWL_EXSTYLE, -20
	wsExToolWindow           = 0x00000080
	dwmwaExtendedFrameBounds = 9
	dwmwaCloaked             = 14
	enumWindowsContinue      = 1
	sizeofRect               = 16
	sizeofUint32             = 4
)

// OnScreenWindows returns the bounds of the normal windows on screen, front
// to back, leaving out arubox's own, tool windows and windows hidden on
// other virtual desktops.
func OnScreenWindows() []Rect {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumRects = nil
	procEnumWindows.Call(enumCallback, 0)
	rects := enumRects
	enumRects = nil
	return rects
}

// A Go callback cannot be freed and only so many can be made, so one is made
// for good, collecting into enumRects under enumMu.
var (
	enumMu       sync.Mutex
	enumRects    []Rect
	enumCallback = windows.NewCallback(collectWindow)
)

func collectWindow(hwnd, _ uintptr) uintptr {
	if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 {
		return enumWindowsContinue
	}
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		return enumWindowsContinue
	}
	if style, _, _ := procGetWindowLongPtrW.Call(hwnd, gwlExStyle); style&wsExToolWindow != 0 {
		return enumWindowsContinue
	}
	var pid uint32
	procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == windows.GetCurrentProcessId() {
		return enumWindowsContinue
	}
	var cloaked uint32
	procDwmGetWindowAttribute.Call(hwnd, dwmwaCloaked, uintptr(unsafe.Pointer(&cloaked)), sizeofUint32)
	if cloaked != 0 {
		return enumWindowsContinue
	}
	// The frame bounds leave out the invisible resize borders.
	var r struct{ Left, Top, Right, Bottom int32 }
	if hr, _, _ := procDwmGetWindowAttribute.Call(hwnd, dwmwaExtendedFrameBounds, uintptr(unsafe.Pointer(&r)), sizeofRect); !succeeded(hr) {
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	}
	if w, h := int(r.Right-r.Left), int(r.Bottom-r.Top); w >= minWindowSize && h >= minWindowSize {
		enumRects = append(enumRects, Rect{X: int(r.Left), Y: int(r.Top), Width: w, Height: h})
	}
	return enumWindowsContinue
}
