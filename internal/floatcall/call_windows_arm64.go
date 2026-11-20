package floatcall

import (
	"fmt"
	"syscall"
	"unsafe"
)

// frame holds the argument registers trampoline loads before it jumps to
// fn. call_windows_arm64.s reads it through the offsets in go_asm.h.
type frame struct {
	fn     uintptr
	ints   [8]uintptr // X0-X7
	floats [8]uint64  // D0-D7; a FLOAT is the low 32 bits
}

// trampolineAddr returns the address of the trampoline in
// call_windows_arm64.s. Called with the Windows calling convention and a
// *frame in X0, it loads the frame's registers and jumps to frame.fn, which
// returns straight to the caller.
func trampolineAddr() uintptr

var trampoline = trampolineAddr()

// Method invokes the method in the given slot of the object's virtual table
// with args, whose kinds are described by kinds (see the package comment).
// Arguments that would go on the stack (a ninth integer or float) are not
// supported; no method called through here takes that many.
//
//go:uintptrescapes
func Method(this unsafe.Pointer, slot int, kinds string, args ...uintptr) uintptr {
	check(kinds, args)
	f := frame{fn: method(this, slot)}
	f.ints[0] = uintptr(this)
	ni, nf := 1, 0
	for i, a := range args {
		switch kinds[i] {
		case 'i':
			if ni == len(f.ints) {
				panic(fmt.Sprintf("floatcall: more than %d integer arguments", len(f.ints)))
			}
			f.ints[ni] = a
			ni++
		case 'f':
			if nf == len(f.floats) {
				panic(fmt.Sprintf("floatcall: more than %d float arguments", len(f.floats)))
			}
			f.floats[nf] = uint64(uint32(a))
			nf++
		case 'p':
			// A D2D1_POINT_2F is a homogeneous float aggregate: its two
			// members take two consecutive float registers.
			if nf+2 > len(f.floats) {
				panic(fmt.Sprintf("floatcall: more than %d float arguments", len(f.floats)))
			}
			f.floats[nf] = uint64(uint32(a))
			f.floats[nf+1] = uint64(uint32(a >> 32))
			nf += 2
		}
	}
	r, _, _ := syscall.SyscallN(trampoline, uintptr(unsafe.Pointer(&f)))
	return r
}
