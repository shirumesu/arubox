package platform

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                       = windows.NewLazySystemDLL("kernel32.dll")
	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procEnumClipboardFormats       = user32.NewProc("EnumClipboardFormats")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procDestroyWindow              = user32.NewProc("DestroyWindow")
	procGlobalAlloc                = kernel32.NewProc("GlobalAlloc")
	procGlobalFree                 = kernel32.NewProc("GlobalFree")
	procGlobalLock                 = kernel32.NewProc("GlobalLock")
	procGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
	procGlobalSize                 = kernel32.NewProc("GlobalSize")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
	hwndMessage   = ^uintptr(2) // HWND_MESSAGE, (HWND)-3
)

// clipboardItem is one format of the clipboard's content.
type clipboardItem struct {
	format uint32
	data   []byte
}

// gdiHandleFormat reports formats whose data is a GDI handle rather than
// memory, which cannot be copied byte for byte. Each has a memory-based
// sibling (CF_DIB for bitmaps) that is saved instead.
func gdiHandleFormat(format uint32) bool {
	switch format {
	case 2, 3, 9, 14, 0x80, 0x82, 0x83, 0x8e: // BITMAP, METAFILEPICT, PALETTE, ENHMETAFILE, OWNERDISPLAY, DSP*
		return true
	}
	return format >= 0x300 && format <= 0x3ff // CF_GDIOBJFIRST..LAST
}

func createMessageWindow() (uintptr, error) {
	class, _ := windows.UTF16PtrFromString("STATIC")
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, hwndMessage, 0, 0, 0)
	if hwnd == 0 {
		return 0, err
	}
	return hwnd, nil
}

// openClipboard retries briefly: another program may be holding it.
func openClipboard(owner uintptr) bool {
	for range 10 {
		if r, _, _ := procOpenClipboard.Call(owner); r != 0 {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// takeClipboard returns the clipboard's contents and empties it.
func takeClipboard(owner uintptr) ([]clipboardItem, bool) {
	if !openClipboard(owner) {
		return nil, false
	}
	defer procCloseClipboard.Call()
	defer procEmptyClipboard.Call()
	var items []clipboardItem
	format := uintptr(0)
	for {
		format, _, _ = procEnumClipboardFormats.Call(format)
		if format == 0 {
			break
		}
		if gdiHandleFormat(uint32(format)) {
			continue
		}
		if data, ok := globalBytes(format); ok {
			items = append(items, clipboardItem{format: uint32(format), data: data})
		}
	}
	return items, true
}

// globalLock returns the memory behind a global handle, or nil.
func globalLock(h uintptr) unsafe.Pointer {
	p, _, _ := procGlobalLock.Call(h)
	return unsafe.Pointer(p) //nolint:govet // memory owned by Windows, outside the Go heap
}

func globalBytes(format uintptr) ([]byte, bool) {
	h, _, _ := procGetClipboardData.Call(format)
	if h == 0 {
		return nil, false
	}
	size, _, _ := procGlobalSize.Call(h)
	p := globalLock(h)
	if p == nil {
		return nil, false
	}
	defer procGlobalUnlock.Call(h)
	return append([]byte(nil), unsafe.Slice((*byte)(p), size)...), true
}

// restoreClipboard replaces the clipboard's contents with items. It reports
// whether the clipboard could be opened.
func restoreClipboard(owner uintptr, items []clipboardItem) bool {
	if !openClipboard(owner) {
		return false
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	for _, item := range items {
		h, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(max(len(item.data), 1)))
		if h == 0 {
			continue
		}
		if p := globalLock(h); p != nil {
			copy(unsafe.Slice((*byte)(p), len(item.data)), item.data)
			procGlobalUnlock.Call(h)
		}
		// The clipboard owns the memory once SetClipboardData succeeds.
		if r, _, _ := procSetClipboardData.Call(uintptr(item.format), h); r == 0 {
			procGlobalFree.Call(h)
		}
	}
	return true
}

// unmonitored adds the format that tells clipboard history and clipboard
// tools to skip this content, for putting the clipboard back as it was.
func unmonitored(items []clipboardItem) []clipboardItem {
	name, _ := windows.UTF16PtrFromString("ExcludeClipboardContentFromMonitorProcessing")
	format, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name)))
	if format == 0 {
		return items
	}
	return append(items, clipboardItem{format: uint32(format)})
}

func clipboardText(owner uintptr) string {
	if !openClipboard(owner) {
		return ""
	}
	defer procCloseClipboard.Call()
	data, ok := globalBytes(cfUnicodeText)
	if !ok || len(data) < 2 {
		return ""
	}
	return windows.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(&data[0])), len(data)/2))
}
