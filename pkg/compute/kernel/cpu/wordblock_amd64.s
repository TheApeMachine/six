//go:build amd64

#include "textflag.h"

// =========================================================================
// AVX2 bulk bitwise operations over []uint64 slices.
//
// All functions share the signature: func(dst, src *uint64, n int)
//   dst+0(FP)  — pointer to destination words
//   src+8(FP)  — pointer to source words
//   n+16(FP)   — number of uint64 words to process
//
// Strategy: 4× YMM unroll (16 words = 128 bytes = 1024 bits per iteration),
// then a 1× YMM middle pass (4 words), then a scalar tail (0–3 words).
//
// A full 128-word Value span completes the hot loop in 8 iterations.
// The 4 independent YMM chains (Y0–Y3 / Y4–Y7) let the CPU pipeline
// loads, ops, and stores across execution ports simultaneously.
// =========================================================================


TEXT ·simdXor(SB), NOSPLIT, $0-24
	MOVQ	dst+0(FP), DI
	MOVQ	src+8(FP), SI
	MOVQ	n+16(FP), CX

	// Hot loop: 16 words per iteration (4× YMM).
	MOVQ	CX, DX
	SHRQ	$4, CX			// CX = n / 16
	JZ	xor_mid_setup
xor_hot:
	VMOVDQU	0*32(SI), Y0
	VMOVDQU	1*32(SI), Y1
	VMOVDQU	2*32(SI), Y2
	VMOVDQU	3*32(SI), Y3
	VPXOR	0*32(DI), Y0, Y0
	VPXOR	1*32(DI), Y1, Y1
	VPXOR	2*32(DI), Y2, Y2
	VPXOR	3*32(DI), Y3, Y3
	VMOVDQU	Y0, 0*32(DI)
	VMOVDQU	Y1, 1*32(DI)
	VMOVDQU	Y2, 2*32(DI)
	VMOVDQU	Y3, 3*32(DI)
	ADDQ	$128, SI
	ADDQ	$128, DI
	DECQ	CX
	JNZ	xor_hot

	// Middle: 4 words at a time from remainder.
xor_mid_setup:
	ANDQ	$15, DX			// DX = n % 16
	MOVQ	DX, CX
	SHRQ	$2, CX			// CX = remainder / 4
	JZ	xor_tail
xor_mid:
	VMOVDQU	(SI), Y0
	VPXOR	(DI), Y0, Y0
	VMOVDQU	Y0, (DI)
	ADDQ	$32, SI
	ADDQ	$32, DI
	DECQ	CX
	JNZ	xor_mid

	// Scalar tail: 0–3 words.
xor_tail:
	ANDQ	$3, DX
	JZ	xor_done
xor_scalar:
	MOVQ	(SI), AX
	XORQ	AX, (DI)
	ADDQ	$8, SI
	ADDQ	$8, DI
	DECQ	DX
	JNZ	xor_scalar
xor_done:
	VZEROUPPER
	RET


TEXT ·simdAnd(SB), NOSPLIT, $0-24
	MOVQ	dst+0(FP), DI
	MOVQ	src+8(FP), SI
	MOVQ	n+16(FP), CX

	MOVQ	CX, DX
	SHRQ	$4, CX
	JZ	and_mid_setup
and_hot:
	VMOVDQU	0*32(SI), Y0
	VMOVDQU	1*32(SI), Y1
	VMOVDQU	2*32(SI), Y2
	VMOVDQU	3*32(SI), Y3
	VPAND	0*32(DI), Y0, Y0
	VPAND	1*32(DI), Y1, Y1
	VPAND	2*32(DI), Y2, Y2
	VPAND	3*32(DI), Y3, Y3
	VMOVDQU	Y0, 0*32(DI)
	VMOVDQU	Y1, 1*32(DI)
	VMOVDQU	Y2, 2*32(DI)
	VMOVDQU	Y3, 3*32(DI)
	ADDQ	$128, SI
	ADDQ	$128, DI
	DECQ	CX
	JNZ	and_hot

and_mid_setup:
	ANDQ	$15, DX
	MOVQ	DX, CX
	SHRQ	$2, CX
	JZ	and_tail
and_mid:
	VMOVDQU	(SI), Y0
	VPAND	(DI), Y0, Y0
	VMOVDQU	Y0, (DI)
	ADDQ	$32, SI
	ADDQ	$32, DI
	DECQ	CX
	JNZ	and_mid

and_tail:
	ANDQ	$3, DX
	JZ	and_done
and_scalar:
	MOVQ	(SI), AX
	ANDQ	AX, (DI)
	ADDQ	$8, SI
	ADDQ	$8, DI
	DECQ	DX
	JNZ	and_scalar
and_done:
	VZEROUPPER
	RET


TEXT ·simdOr(SB), NOSPLIT, $0-24
	MOVQ	dst+0(FP), DI
	MOVQ	src+8(FP), SI
	MOVQ	n+16(FP), CX

	MOVQ	CX, DX
	SHRQ	$4, CX
	JZ	or_mid_setup
or_hot:
	VMOVDQU	0*32(SI), Y0
	VMOVDQU	1*32(SI), Y1
	VMOVDQU	2*32(SI), Y2
	VMOVDQU	3*32(SI), Y3
	VPOR	0*32(DI), Y0, Y0
	VPOR	1*32(DI), Y1, Y1
	VPOR	2*32(DI), Y2, Y2
	VPOR	3*32(DI), Y3, Y3
	VMOVDQU	Y0, 0*32(DI)
	VMOVDQU	Y1, 1*32(DI)
	VMOVDQU	Y2, 2*32(DI)
	VMOVDQU	Y3, 3*32(DI)
	ADDQ	$128, SI
	ADDQ	$128, DI
	DECQ	CX
	JNZ	or_hot

or_mid_setup:
	ANDQ	$15, DX
	MOVQ	DX, CX
	SHRQ	$2, CX
	JZ	or_tail
or_mid:
	VMOVDQU	(SI), Y0
	VPOR	(DI), Y0, Y0
	VMOVDQU	Y0, (DI)
	ADDQ	$32, SI
	ADDQ	$32, DI
	DECQ	CX
	JNZ	or_mid

or_tail:
	ANDQ	$3, DX
	JZ	or_done
or_scalar:
	MOVQ	(SI), AX
	ORQ	AX, (DI)
	ADDQ	$8, SI
	ADDQ	$8, DI
	DECQ	DX
	JNZ	or_scalar
or_done:
	VZEROUPPER
	RET


TEXT ·simdSrcAndNotDst(SB), NOSPLIT, $0-24
	MOVQ	dst+0(FP), DI
	MOVQ	src+8(FP), SI
	MOVQ	n+16(FP), CX

	MOVQ	CX, DX
	SHRQ	$4, CX
	JZ	sandn_mid_setup
sandn_hot:
	VMOVDQU	0*32(DI), Y0		// dst (complemented by VPANDN)
	VMOVDQU	1*32(DI), Y1
	VMOVDQU	2*32(DI), Y2
	VMOVDQU	3*32(DI), Y3
	VMOVDQU	0*32(SI), Y4		// src
	VMOVDQU	1*32(SI), Y5
	VMOVDQU	2*32(SI), Y6
	VMOVDQU	3*32(SI), Y7
	VPANDN	Y0, Y4, Y0		// ~dst & src
	VPANDN	Y1, Y5, Y1
	VPANDN	Y2, Y6, Y2
	VPANDN	Y3, Y7, Y3
	VMOVDQU	Y0, 0*32(DI)
	VMOVDQU	Y1, 1*32(DI)
	VMOVDQU	Y2, 2*32(DI)
	VMOVDQU	Y3, 3*32(DI)
	ADDQ	$128, SI
	ADDQ	$128, DI
	DECQ	CX
	JNZ	sandn_hot

sandn_mid_setup:
	ANDQ	$15, DX
	MOVQ	DX, CX
	SHRQ	$2, CX
	JZ	sandn_tail
sandn_mid:
	VMOVDQU	(DI), Y0
	VMOVDQU	(SI), Y1
	VPANDN	Y0, Y1, Y0
	VMOVDQU	Y0, (DI)
	ADDQ	$32, SI
	ADDQ	$32, DI
	DECQ	CX
	JNZ	sandn_mid

sandn_tail:
	ANDQ	$3, DX
	JZ	sandn_done
sandn_scalar:
	MOVQ	(DI), AX
	MOVQ	(SI), BX
	NOTQ	AX
	ANDQ	BX, AX
	MOVQ	AX, (DI)
	ADDQ	$8, SI
	ADDQ	$8, DI
	DECQ	DX
	JNZ	sandn_scalar
sandn_done:
	VZEROUPPER
	RET


TEXT ·simdDstAndNotSrc(SB), NOSPLIT, $0-24
	MOVQ	dst+0(FP), DI
	MOVQ	src+8(FP), SI
	MOVQ	n+16(FP), CX

	MOVQ	CX, DX
	SHRQ	$4, CX
	JZ	dandn_mid_setup
dandn_hot:
	VMOVDQU	0*32(SI), Y0		// src (complemented by VPANDN)
	VMOVDQU	1*32(SI), Y1
	VMOVDQU	2*32(SI), Y2
	VMOVDQU	3*32(SI), Y3
	VMOVDQU	0*32(DI), Y4		// dst
	VMOVDQU	1*32(DI), Y5
	VMOVDQU	2*32(DI), Y6
	VMOVDQU	3*32(DI), Y7
	VPANDN	Y0, Y4, Y0		// ~src & dst
	VPANDN	Y1, Y5, Y1
	VPANDN	Y2, Y6, Y2
	VPANDN	Y3, Y7, Y3
	VMOVDQU	Y0, 0*32(DI)
	VMOVDQU	Y1, 1*32(DI)
	VMOVDQU	Y2, 2*32(DI)
	VMOVDQU	Y3, 3*32(DI)
	ADDQ	$128, SI
	ADDQ	$128, DI
	DECQ	CX
	JNZ	dandn_hot

dandn_mid_setup:
	ANDQ	$15, DX
	MOVQ	DX, CX
	SHRQ	$2, CX
	JZ	dandn_tail
dandn_mid:
	VMOVDQU	(SI), Y0
	VMOVDQU	(DI), Y1
	VPANDN	Y0, Y1, Y0
	VMOVDQU	Y0, (DI)
	ADDQ	$32, SI
	ADDQ	$32, DI
	DECQ	CX
	JNZ	dandn_mid

dandn_tail:
	ANDQ	$3, DX
	JZ	dandn_done
dandn_scalar:
	MOVQ	(SI), AX
	NOTQ	AX
	ANDQ	AX, (DI)
	ADDQ	$8, SI
	ADDQ	$8, DI
	DECQ	DX
	JNZ	dandn_scalar
dandn_done:
	VZEROUPPER
	RET


DATA popcnt_lut<>+0(SB)/8,  $0x0403030203020201
DATA popcnt_lut<>+8(SB)/8,  $0x0504040304030302
DATA popcnt_lut<>+16(SB)/8, $0x0403030203020201
DATA popcnt_lut<>+24(SB)/8, $0x0504040304030302
GLOBL popcnt_lut<>(SB), RODATA, $32

DATA mask_0f<>+0(SB)/8,  $0x0F0F0F0F0F0F0F0F
DATA mask_0f<>+8(SB)/8,  $0x0F0F0F0F0F0F0F0F
DATA mask_0f<>+16(SB)/8, $0x0F0F0F0F0F0F0F0F
DATA mask_0f<>+24(SB)/8, $0x0F0F0F0F0F0F0F0F
GLOBL mask_0f<>(SB), RODATA, $32


TEXT ·simdPopcnt(SB), NOSPLIT, $0-24
	MOVQ	dst+0(FP), DI
	MOVQ	src+8(FP), SI
	MOVQ	n+16(FP), CX

	VPXOR	Y15, Y15, Y15
	VMOVDQU	popcnt_lut<>(SB), Y14
	VMOVDQU	mask_0f<>(SB), Y13

	MOVQ	CX, DX
	SHRQ	$2, CX
	JZ		popcnt_tail
popcnt_hot:
	VMOVDQU	(SI), Y0
	VMOVDQU	(DI), Y1
	VPXOR	Y0, Y1, Y0

	VMOVDQU	Y0, Y1
	VPSRLW	$4, Y1, Y1

	VPAND	Y13, Y1, Y1
	VPAND	Y13, Y0, Y0

	VPSHUFB	Y1, Y14, Y1
	VPSHUFB	Y0, Y14, Y0

	VPADDB	Y1, Y0, Y0
	VPSADBW	Y15, Y0, Y0

	VMOVDQU	Y0, (DI)

	ADDQ	$32, SI
	ADDQ	$32, DI
	DECQ	CX
	JNZ		popcnt_hot

popcnt_tail:
	ANDQ	$3, DX
	JZ		popcnt_done
popcnt_scalar:
	MOVQ	(SI), AX
	XORQ	(DI), AX
	POPCNTQ	AX, AX
	MOVQ	AX, (DI)
	ADDQ	$8, SI
	ADDQ	$8, DI
	DECQ	DX
	JNZ		popcnt_scalar
popcnt_done:
	VZEROUPPER
	RET


TEXT ·simdShl(SB), NOSPLIT, $0-24
	MOVQ	dst+0(FP), DI
	MOVQ	src+8(FP), SI
	MOVQ	n+16(FP), CX

	MOVQ	CX, DX
	SHRQ	$4, CX
	JZ	shl_mid_setup
shl_hot:
	VMOVDQU	0*32(SI), Y0
	VMOVDQU	1*32(SI), Y1
	VMOVDQU	2*32(SI), Y2
	VMOVDQU	3*32(SI), Y3
	VMOVDQU	0*32(DI), Y4
	VMOVDQU	1*32(DI), Y5
	VMOVDQU	2*32(DI), Y6
	VMOVDQU	3*32(DI), Y7
	VPSLLVQ	Y0, Y4, Y0
	VPSLLVQ	Y1, Y5, Y1
	VPSLLVQ	Y2, Y6, Y2
	VPSLLVQ	Y3, Y7, Y3
	VMOVDQU	Y0, 0*32(DI)
	VMOVDQU	Y1, 1*32(DI)
	VMOVDQU	Y2, 2*32(DI)
	VMOVDQU	Y3, 3*32(DI)
	ADDQ	$128, SI
	ADDQ	$128, DI
	DECQ	CX
	JNZ	shl_hot
shl_mid_setup:
	ANDQ	$15, DX
	MOVQ	DX, CX
	SHRQ	$2, CX
	JZ	shl_tail
shl_mid:
	VMOVDQU	(SI), Y0
	VMOVDQU	(DI), Y1
	VPSLLVQ	Y0, Y1, Y0
	VMOVDQU	Y0, (DI)
	ADDQ	$32, SI
	ADDQ	$32, DI
	DECQ	CX
	JNZ	shl_mid
shl_tail:
	ANDQ	$3, DX
	JZ	shl_done
shl_scalar:
	MOVQ	(SI), CX
	ANDQ	$63, CX
	MOVQ	(DI), AX
	SHLQ	CX, AX
	MOVQ	AX, (DI)
	ADDQ	$8, SI
	ADDQ	$8, DI
	DECQ	DX
	JNZ	shl_scalar
shl_done:
	VZEROUPPER
	RET


TEXT ·simdShr(SB), NOSPLIT, $0-24
	MOVQ	dst+0(FP), DI
	MOVQ	src+8(FP), SI
	MOVQ	n+16(FP), CX

	MOVQ	CX, DX
	SHRQ	$4, CX
	JZ	shr_mid_setup
shr_hot:
	VMOVDQU	0*32(SI), Y0
	VMOVDQU	1*32(SI), Y1
	VMOVDQU	2*32(SI), Y2
	VMOVDQU	3*32(SI), Y3
	VMOVDQU	0*32(DI), Y4
	VMOVDQU	1*32(DI), Y5
	VMOVDQU	2*32(DI), Y6
	VMOVDQU	3*32(DI), Y7
	VPSRLVQ	Y0, Y4, Y0
	VPSRLVQ	Y1, Y5, Y1
	VPSRLVQ	Y2, Y6, Y2
	VPSRLVQ	Y3, Y7, Y3
	VMOVDQU	Y0, 0*32(DI)
	VMOVDQU	Y1, 1*32(DI)
	VMOVDQU	Y2, 2*32(DI)
	VMOVDQU	Y3, 3*32(DI)
	ADDQ	$128, SI
	ADDQ	$128, DI
	DECQ	CX
	JNZ	shr_hot
shr_mid_setup:
	ANDQ	$15, DX
	MOVQ	DX, CX
	SHRQ	$2, CX
	JZ	shr_tail
shr_mid:
	VMOVDQU	(SI), Y0
	VMOVDQU	(DI), Y1
	VPSRLVQ	Y0, Y1, Y0
	VMOVDQU	Y0, (DI)
	ADDQ	$32, SI
	ADDQ	$32, DI
	DECQ	CX
	JNZ	shr_mid
shr_tail:
	ANDQ	$3, DX
	JZ	shr_done
shr_scalar:
	MOVQ	(SI), CX
	ANDQ	$63, CX
	MOVQ	(DI), AX
	SHRQ	CX, AX
	MOVQ	AX, (DI)
	ADDQ	$8, SI
	ADDQ	$8, DI
	DECQ	DX
	JNZ	shr_scalar
shr_done:
	VZEROUPPER
	RET
