package platform

import (
	"encoding/binary"
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procRegisterClipboardFormatW = user32.NewProc("RegisterClipboardFormatW")

const cfDIB = 8

// CopyImage puts an opaque picture on the clipboard as a device-independent
// bitmap, which every program reads, and as PNG, which some programs
// prefer. pngData is the same picture encoded as PNG. The bitmap is made
// from img's pixels as they are, rather than by decoding pngData again,
// which took two more copies of a picture that can be screen sized.
func CopyImage(img Image, pngData []byte) error {
	if img.Width <= 0 || img.Height <= 0 || len(img.Pixels) < img.Width*img.Height*4 {
		return errors.New("没有图片")
	}
	name, err := windows.UTF16PtrFromString("PNG")
	if err != nil {
		return err
	}
	pngFormat, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name)))
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	owner, err := createMessageWindow()
	if err != nil {
		return err
	}
	defer procDestroyWindow.Call(owner)
	items := []clipboardItem{{format: cfDIB, data: dib(img)}}
	if pngFormat != 0 {
		items = append(items, clipboardItem{format: uint32(pngFormat), data: pngData})
	}
	if !restoreClipboard(owner, items) {
		return errors.New("无法写入剪贴板")
	}
	return nil
}

// dib encodes top-down BGRA pixels as a 32-bit bottom-up device-independent
// bitmap, whose rows are BGRA too.
func dib(img Image) []byte {
	w, h := img.Width, img.Height
	out := make([]byte, bitmapInfoHeaderBytes+w*h*4)
	binary.LittleEndian.PutUint32(out[0:], bitmapInfoHeaderBytes)
	binary.LittleEndian.PutUint32(out[4:], uint32(w))
	binary.LittleEndian.PutUint32(out[8:], uint32(h))
	binary.LittleEndian.PutUint16(out[12:], 1)
	binary.LittleEndian.PutUint16(out[14:], 32)
	rows := out[bitmapInfoHeaderBytes:]
	for y := range h {
		copy(rows[(h-1-y)*w*4:(h-y)*w*4], img.Pixels[y*w*4:(y+1)*w*4])
	}
	return out
}
