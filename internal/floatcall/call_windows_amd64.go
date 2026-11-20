package floatcall

import (
	"syscall"
	"unsafe"
)

// Method invokes the method in the given slot of the object's virtual table
// with args, whose kinds are described by kinds (see the package comment).
// On amd64 the integer form is already right for floats, so kinds is only
// checked.
//
//go:uintptrescapes
func Method(this unsafe.Pointer, slot int, kinds string, args ...uintptr) uintptr {
	check(kinds, args)
	r, _, _ := syscall.SyscallN(method(this, slot), append([]uintptr{uintptr(this)}, args...)...)
	return r
}
