package ui

import (
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

// pixelLock is an IWICBitmapLock over an image's pixels, which Direct2D
// takes to make a bitmap that reads them in place. It keeps the pixel
// slice alive for as long as Direct2D holds it.
type pixelLock struct {
	vtbl   *[7]uintptr
	refs   int32
	width  uint32
	height uint32
	px     []byte
}

var (
	iidIUnknown       = windows.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIWICBitmapLock = windows.GUID{Data1: 0x00000123, Data2: 0xa8f2, Data3: 0x4877, Data4: [8]byte{0xba, 0x0a, 0xfd, 0x2b, 0x66, 0x45, 0xfb, 0x94}}

	pixelLockVtbl = [7]uintptr{
		windows.NewCallback(lockQueryInterface),
		windows.NewCallback(lockAddRef),
		windows.NewCallback(lockRelease),
		windows.NewCallback(lockGetSize),
		windows.NewCallback(lockGetStride),
		windows.NewCallback(lockGetDataPointer),
		windows.NewCallback(lockGetPixelFormat),
	}
	// pixelLocks keeps the locks reachable while COM holds them.
	pixelLocks = map[uintptr]*pixelLock{}
)

const (
	sOK          = 0
	eNoInterface = 0x80004002
)

// newPixelLock returns a lock with one reference, for the caller to
// release.
func newPixelLock(img *Image) uintptr {
	l := &pixelLock{vtbl: &pixelLockVtbl, refs: 1, width: uint32(img.Width), height: uint32(img.Height), px: img.Pixels}
	p := uintptr(unsafe.Pointer(l))
	pixelLocks[p] = l
	return p
}

func lockQueryInterface(this uintptr, iid *windows.GUID, out *uintptr) uintptr {
	if *iid == iidIUnknown || *iid == iidIWICBitmapLock {
		*out = this
		lockAddRef(this)
		return sOK
	}
	*out = 0
	return eNoInterface
}

func lockAddRef(this uintptr) uintptr {
	return uintptr(atomic.AddInt32(&pixelLocks[this].refs, 1))
}

func lockRelease(this uintptr) uintptr {
	n := atomic.AddInt32(&pixelLocks[this].refs, -1)
	if n == 0 {
		delete(pixelLocks, this)
	}
	return uintptr(n)
}

func lockGetSize(this uintptr, w, h *uint32) uintptr {
	l := pixelLocks[this]
	*w, *h = l.width, l.height
	return sOK
}

func lockGetStride(this uintptr, stride *uint32) uintptr {
	*stride = pixelLocks[this].width * 4
	return sOK
}

func lockGetDataPointer(this uintptr, size *uint32, data *uintptr) uintptr {
	l := pixelLocks[this]
	*size = uint32(len(l.px))
	*data = uintptr(unsafe.Pointer(&l.px[0]))
	return sOK
}

func lockGetPixelFormat(this uintptr, format *windows.GUID) uintptr {
	*format = guidWICPixelFormat32bppPBGRA
	return sOK
}
