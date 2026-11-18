package platform

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// CopyText puts text on the clipboard.
func CopyText(text string) error {
	u, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	size := uintptr(len(u) * 2)
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return errors.New("复制失败")
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return errors.New("复制失败")
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(u)), u)
	procGlobalUnlock.Call(h)
	if !openClipboard(0) {
		procGlobalFree.Call(h)
		return errors.New("剪贴板被其他程序占用")
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	if r, _, _ := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		procGlobalFree.Call(h)
		return errors.New("复制失败")
	}
	return nil
}
