#include "go_asm.h"
#include "textflag.h"

// func trampolineAddr() uintptr
TEXT ·trampolineAddr(SB),NOSPLIT,$0-8
	MOVD	$trampoline<>(SB), R0
	MOVD	R0, ret+0(FP)
	RET

// trampoline is reached through syscall.SyscallN, so it runs under the
// Windows calling convention with a *frame in R0. It loads D0-D7 and X0-X7
// from the frame and jumps to frame.fn; the method returns to SyscallN with
// its result in X0. R9 is a caller-saved scratch register.
TEXT trampoline<>(SB),NOSPLIT|NOFRAME,$0
	MOVD	R0, R9
	FMOVD	(frame_floats+0*8)(R9), F0
	FMOVD	(frame_floats+1*8)(R9), F1
	FMOVD	(frame_floats+2*8)(R9), F2
	FMOVD	(frame_floats+3*8)(R9), F3
	FMOVD	(frame_floats+4*8)(R9), F4
	FMOVD	(frame_floats+5*8)(R9), F5
	FMOVD	(frame_floats+6*8)(R9), F6
	FMOVD	(frame_floats+7*8)(R9), F7
	MOVD	(frame_ints+0*8)(R9), R0
	MOVD	(frame_ints+1*8)(R9), R1
	MOVD	(frame_ints+2*8)(R9), R2
	MOVD	(frame_ints+3*8)(R9), R3
	MOVD	(frame_ints+4*8)(R9), R4
	MOVD	(frame_ints+5*8)(R9), R5
	MOVD	(frame_ints+6*8)(R9), R6
	MOVD	(frame_ints+7*8)(R9), R7
	MOVD	frame_fn(R9), R9
	B	(R9)
