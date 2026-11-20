// Package floatcall calls COM methods that take FLOAT arguments by value.
//
// syscall.SyscallN passes every argument as an integer. On amd64 that is
// enough: Windows x64 gives each argument a position, and the runtime copies
// the first four into the XMM registers as well. Windows ARM64 assigns
// integer and floating-point arguments to separate register files
// (X0-X7 and V0-V7), so a float passed as an integer lands in the wrong
// register and shifts every argument after it. Method is told which
// arguments are floats and places them as the platform expects.
package floatcall

import (
	"fmt"
	"unsafe"
)

// Argument kinds, one letter per argument after the object:
//
//	'i'  an integer or pointer
//	'f'  a FLOAT, given as its bits (math.Float32bits)
//	'p'  a D2D1_POINT_2F, given as x | y<<32 (each as its bits)

// method returns the address of the method in the given slot of the
// object's virtual table.
func method(this unsafe.Pointer, slot int) uintptr {
	vtable := *(*unsafe.Pointer)(this)
	return *(*uintptr)(unsafe.Add(vtable, uintptr(slot)*unsafe.Sizeof(uintptr(0))))
}

// check panics unless kinds classes each argument, so a mistake shows on
// any architecture, not only on the one it would break.
func check(kinds string, args []uintptr) {
	if len(kinds) != len(args) {
		panic(fmt.Sprintf("floatcall: %d kinds for %d arguments", len(kinds), len(args)))
	}
	for _, k := range kinds {
		if k != 'i' && k != 'f' && k != 'p' {
			panic(fmt.Sprintf("floatcall: unknown argument kind %q", k))
		}
	}
}
