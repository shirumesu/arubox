// Package pixellock lends Direct2D a picture's pixels in place.
//
// ID2D1RenderTarget::CreateSharedBitmap takes an IWICBitmapLock and, on a
// software render target, makes a bitmap that reads the lock's memory
// rather than a copy of it. A copy made by CreateBitmap would land in the
// software rasterizer's pool, which keeps its buffers for the life of the
// process.
package pixellock

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// IID is IID_IWICBitmapLock, for CreateSharedBitmap.
var IID = windows.GUID{Data1: 0x00000123, Data2: 0xa8f2, Data3: 0x4877, Data4: [8]byte{0xba, 0x0a, 0xfd, 0x2b, 0x66, 0x45, 0xfb, 0x94}}

// lock is an IWICBitmapLock over a picture's pixels. It keeps the pixel
// slice alive for as long as Direct2D holds it.
type lock struct {
	vtbl   *[7]uintptr
	refs   int32
	width  uint32
	height uint32
	px     []byte
}

var (
	iidIUnknown                  = windows.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	guidWICPixelFormat32bppPBGRA = windows.GUID{Data1: 0x6fddc324, Data2: 0x4e03, Data3: 0x4bfe, Data4: [8]byte{0xb1, 0x85, 0x3d, 0x77, 0x76, 0x8d, 0xc9, 0x10}}

	vtbl = [7]uintptr{
		windows.NewCallback(queryInterface),
		windows.NewCallback(addRef),
		windows.NewCallback(release),
		windows.NewCallback(getSize),
		windows.NewCallback(getStride),
		windows.NewCallback(getDataPointer),
		windows.NewCallback(getPixelFormat),
	}
	// locks keeps the locks reachable while COM holds them. The UI thread
	// and the desktop's thread each make their own, so it is guarded.
	locksMu sync.Mutex
	locks   = map[uintptr]*lock{}
)

const (
	sOK          = 0
	eNoInterface = 0x80004002
)

// New returns a lock over width by height top-down pixels in premultiplied
// BGRA, width*4 bytes a row, with one reference for the caller to Release.
// The pixels must not change while a bitmap made from it is in use.
func New(px []byte, width, height int) uintptr {
	l := &lock{vtbl: &vtbl, refs: 1, width: uint32(width), height: uint32(height), px: px}
	p := uintptr(unsafe.Pointer(l))
	locksMu.Lock()
	locks[p] = l
	locksMu.Unlock()
	return p
}

func lockOf(this uintptr) *lock {
	locksMu.Lock()
	defer locksMu.Unlock()
	return locks[this]
}

// Release gives up the reference New returned.
func Release(p uintptr) { release(p) }

func queryInterface(this uintptr, iid *windows.GUID, out *uintptr) uintptr {
	if *iid == iidIUnknown || *iid == IID {
		*out = this
		addRef(this)
		return sOK
	}
	*out = 0
	return eNoInterface
}

func addRef(this uintptr) uintptr {
	locksMu.Lock()
	defer locksMu.Unlock()
	l := locks[this]
	l.refs++
	return uintptr(l.refs)
}

func release(this uintptr) uintptr {
	locksMu.Lock()
	defer locksMu.Unlock()
	l := locks[this]
	l.refs--
	if l.refs == 0 {
		delete(locks, this)
	}
	return uintptr(l.refs)
}

func getSize(this uintptr, w, h *uint32) uintptr {
	l := lockOf(this)
	*w, *h = l.width, l.height
	return sOK
}

func getStride(this uintptr, stride *uint32) uintptr {
	*stride = lockOf(this).width * 4
	return sOK
}

func getDataPointer(this uintptr, size *uint32, data *uintptr) uintptr {
	l := lockOf(this)
	*size = uint32(len(l.px))
	*data = uintptr(unsafe.Pointer(&l.px[0]))
	return sOK
}

func getPixelFormat(this uintptr, format *windows.GUID) uintptr {
	*format = guidWICPixelFormat32bppPBGRA
	return sOK
}
