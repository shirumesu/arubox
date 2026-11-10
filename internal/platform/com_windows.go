package platform

import (
	"syscall"
	"unsafe"
)

// slotRelease is IUnknown::Release, the same slot in every COM interface.
const slotRelease = 2

// comObject is a COM interface pointer.
type comObject unsafe.Pointer

// call invokes the method in the given slot of the object's virtual table.
// Arguments are often pointers to out-parameters converted to uintptr; the
// directive keeps what they point to on the heap for the call, as for
// windows.LazyProc.Call. On the stack, a variable could move when the stack
// grows, and the method would write its result to where it used to be.
//
//go:uintptrescapes
func call(o comObject, slot int, args ...uintptr) uintptr {
	vtable := *(*unsafe.Pointer)(o)
	method := *(*uintptr)(unsafe.Add(vtable, uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(method, append([]uintptr{uintptr(o)}, args...)...)
	return r
}

func release(o comObject) {
	if o != nil {
		call(o, slotRelease)
	}
}

// succeeded reports whether an HRESULT means success.
func succeeded(hr uintptr) bool { return int32(hr) >= 0 }
