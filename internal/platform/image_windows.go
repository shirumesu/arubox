package platform

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
	"image/png"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procRegisterClipboardFormatW = user32.NewProc("RegisterClipboardFormatW")

const cfDIB = 8

// CopyImage puts a PNG on the clipboard, both as PNG, which keeps
// transparency, and as a device-independent bitmap, which every program
// reads.
func CopyImage(data []byte) error {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return err
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
		items = append(items, clipboardItem{format: uint32(pngFormat), data: data})
	}
	if !restoreClipboard(owner, items) {
		return errors.New("无法写入剪贴板")
	}
	return nil
}

// dib encodes an image as a 32-bit bottom-up device-independent bitmap.
func dib(img image.Image) []byte {
	b := img.Bounds()
	rgba := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	w, h := b.Dx(), b.Dy()
	out := make([]byte, bitmapInfoHeaderBytes, bitmapInfoHeaderBytes+w*h*4)
	binary.LittleEndian.PutUint32(out[0:], bitmapInfoHeaderBytes)
	binary.LittleEndian.PutUint32(out[4:], uint32(w))
	binary.LittleEndian.PutUint32(out[8:], uint32(h))
	binary.LittleEndian.PutUint16(out[12:], 1)
	binary.LittleEndian.PutUint16(out[14:], 32)
	for y := h - 1; y >= 0; y-- {
		row := rgba.Pix[y*rgba.Stride : y*rgba.Stride+w*4]
		for x := 0; x < w*4; x += 4 {
			out = append(out, row[x+2], row[x+1], row[x], row[x+3])
		}
	}
	return out
}
