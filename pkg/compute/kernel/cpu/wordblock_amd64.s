//go:build amd64

#include "textflag.h"

// =========================================================================
// AMD64 / AVX2 SIMD kernels for Six CPU backend.
//
// Three functions:
//   popcount          — popcount(dst[i] ^ src[i]) per word
//   hammingMatch        — early-exit Hamming distance scan
//   universalBitwise    — truth table across 64-word A×B surface
// =========================================================================


// Lookup tables for nibble-level popcount (Harley-Seal).
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


// -------------------------------------------------------------------------
// dst[i] = popcount(dst[i] ^ src[i])
// func(dst, src *uint64, n int)
// -------------------------------------------------------------------------
TEXT ·popcount(SB), NOSPLIT, $0-24
	MOVQ	dst+0(FP), DI
	MOVQ	src+8(FP), SI
	MOVQ	n+16(FP), CX

	VPXOR		Y15, Y15, Y15
	VMOVDQU		popcnt_lut<>(SB), Y14
	VMOVDQU		mask_0f<>(SB), Y13

	MOVQ	CX, DX
	SHRQ	$4, CX
	JZ	popcnt_mid_setup

popcnt_hot:
	VMOVDQU	0*32(SI), Y0
	VMOVDQU	1*32(SI), Y1
	VMOVDQU	2*32(SI), Y2
	VMOVDQU	3*32(SI), Y3
	VPXOR	0*32(DI), Y0, Y0
	VPXOR	1*32(DI), Y1, Y1
	VPXOR	2*32(DI), Y2, Y2
	VPXOR	3*32(DI), Y3, Y3

	VMOVDQU	Y0, Y8
	VPSRLW	$4, Y8, Y8
	VPAND	Y13, Y8, Y8
	VPAND	Y13, Y0, Y0
	VPSHUFB	Y8,  Y14, Y8
	VPSHUFB	Y0,  Y14, Y0
	VPADDB	Y8,  Y0, Y0
	VPSADBW	Y15, Y0, Y0

	VMOVDQU	Y1, Y9
	VPSRLW	$4, Y9, Y9
	VPAND	Y13, Y9, Y9
	VPAND	Y13, Y1, Y1
	VPSHUFB	Y9,  Y14, Y9
	VPSHUFB	Y1,  Y14, Y1
	VPADDB	Y9,  Y1, Y1
	VPSADBW	Y15, Y1, Y1

	VMOVDQU	Y2, Y10
	VPSRLW	$4, Y10, Y10
	VPAND	Y13, Y10, Y10
	VPAND	Y13, Y2,  Y2
	VPSHUFB	Y10, Y14, Y10
	VPSHUFB	Y2,  Y14, Y2
	VPADDB	Y10, Y2,  Y2
	VPSADBW	Y15, Y2,  Y2

	VMOVDQU	Y3, Y11
	VPSRLW	$4, Y11, Y11
	VPAND	Y13, Y11, Y11
	VPAND	Y13, Y3,  Y3
	VPSHUFB	Y11, Y14, Y11
	VPSHUFB	Y3,  Y14, Y3
	VPADDB	Y11, Y3,  Y3
	VPSADBW	Y15, Y3,  Y3

	VMOVDQU	Y0, 0*32(DI)
	VMOVDQU	Y1, 1*32(DI)
	VMOVDQU	Y2, 2*32(DI)
	VMOVDQU	Y3, 3*32(DI)
	ADDQ	$128, SI
	ADDQ	$128, DI
	DECQ	CX
	JNZ	popcnt_hot

popcnt_mid_setup:
	ANDQ	$15, DX
	MOVQ	DX, CX
	SHRQ	$2, CX
	JZ	popcnt_tail
popcnt_mid:
	VMOVDQU	(SI), Y0
	VPXOR	(DI), Y0, Y0
	VMOVDQU	Y0, Y1
	VPSRLW	$4, Y1, Y1
	VPAND	Y13, Y1, Y1
	VPAND	Y13, Y0, Y0
	VPSHUFB	Y1,  Y14, Y1
	VPSHUFB	Y0,  Y14, Y0
	VPADDB	Y1,  Y0, Y0
	VPSADBW	Y15, Y0, Y0
	VMOVDQU	Y0, (DI)
	ADDQ	$32, SI
	ADDQ	$32, DI
	DECQ	CX
	JNZ	popcnt_mid

popcnt_tail:
	ANDQ	$3, DX
	JZ	popcnt_done
popcnt_scalar:
	MOVQ	(SI), AX
	XORQ	(DI), AX
	POPCNTQ	AX, AX
	MOVQ	AX, (DI)
	ADDQ	$8, SI
	ADDQ	$8, DI
	DECQ	DX
	JNZ	popcnt_scalar
popcnt_done:
	VZEROUPPER
	RET


// -------------------------------------------------------------------------
// func(frame *uint64, n int, target uint64, maxDist uint64) bool
// Returns true if any word has popcount(word ^ target) <= maxDist.
// -------------------------------------------------------------------------
TEXT ·hammingMatch(SB), NOSPLIT, $0-33
	MOVQ	frame+0(FP), SI
	MOVQ	n+8(FP), CX
	MOVQ	target+16(FP), R8
	MOVQ	maxDist+24(FP), R9

	VMOVQ		R8, X2
	VPBROADCASTQ	X2, Y2
	VMOVQ		R9, X3
	VPBROADCASTQ	X3, Y3

	VMOVDQU		popcnt_lut<>(SB), Y14
	VMOVDQU		mask_0f<>(SB), Y13
	VPXOR		Y15, Y15, Y15

	MOVQ	CX, DX
	SHRQ	$2, CX
	JZ	hmatch_tail

hmatch_hot:
	VMOVDQU	(SI), Y0
	VPXOR	Y2, Y0, Y0

	VMOVDQU	Y0, Y1
	VPSRLW	$4, Y1, Y1
	VPAND	Y13, Y1, Y1
	VPAND	Y13, Y0, Y0
	VPSHUFB	Y1,  Y14, Y1
	VPSHUFB	Y0,  Y14, Y0
	VPADDB	Y1,  Y0, Y0
	VPSADBW	Y15, Y0, Y0

	VPCMPGTQ	Y3, Y0, Y1
	VPMOVMSKB	Y1, AX
	CMPL		AX, $-1
	JE		hmatch_next
	MOVB	$1, ret+32(FP)
	VZEROUPPER
	RET

hmatch_next:
	ADDQ	$32, SI
	DECQ	CX
	JNZ	hmatch_hot

hmatch_tail:
	ANDQ	$3, DX
	JZ	hmatch_done
hmatch_scalar:
	MOVQ	(SI), AX
	XORQ	R8, AX
	POPCNTQ	AX, AX
	CMPQ	AX, R9
	JBE	hmatch_found
	ADDQ	$8, SI
	DECQ	DX
	JNZ	hmatch_scalar
	JMP	hmatch_done

hmatch_found:
	MOVB	$1, ret+32(FP)
	VZEROUPPER
	RET

hmatch_done:
	MOVB	$0, ret+32(FP)
	VZEROUPPER
	RET


// =========================================================================
// universalBitwise: AVX2 SIMD truth table across 64-word A×B surface.
//
// Signature: func(dst, a, b, m0, m1, m2, m3 *uint64)
//   dst+0(FP)  — pointer to 8 uint64s (output signals)
//   a+8(FP)    — pointer to 64 uint64s (A surface)
//   b+16(FP)   — pointer to 64 uint64s (B surface)
//   m0+24(FP)  — pointer to 64 uint64s (mask for truth table bit 0)
//   m1+32(FP)  — pointer to 64 uint64s (mask for truth table bit 1)
//   m2+40(FP)  — pointer to 64 uint64s (mask for truth table bit 2)
//   m3+48(FP)  — pointer to 64 uint64s (mask for truth table bit 3)
//
// Processes 4 uint64s per YMM register, 16 iterations for 64 elements.
// =========================================================================
TEXT ·universalBitwise(SB), NOSPLIT, $0-56
	MOVQ	dst+0(FP), DI
	MOVQ	a+8(FP), SI
	MOVQ	b+16(FP), DX
	MOVQ	m0+24(FP), R8
	MOVQ	m1+32(FP), R9
	MOVQ	m2+40(FP), R10
	MOVQ	m3+48(FP), R11

	// Zero the 8 output words.
	MOVQ	$0, 0*8(DI)
	MOVQ	$0, 1*8(DI)
	MOVQ	$0, 2*8(DI)
	MOVQ	$0, 3*8(DI)
	MOVQ	$0, 4*8(DI)
	MOVQ	$0, 5*8(DI)
	MOVQ	$0, 6*8(DI)
	MOVQ	$0, 7*8(DI)

	// Y15 = all ones for NOT.
	VPCMPEQD	Y15, Y15, Y15

	// CX = group index (0..15).
	XORQ	CX, CX

ub_avx_loop:
	MOVQ	CX, AX
	SHLQ	$5, AX			// AX = CX * 32

	VMOVDQU	(SI)(AX*1), Y0		// a
	VMOVDQU	(DX)(AX*1), Y1		// b
	VMOVDQU	(R8)(AX*1), Y8		// m0
	VMOVDQU	(R9)(AX*1), Y9		// m1
	VMOVDQU	(R10)(AX*1), Y10	// m2
	VMOVDQU	(R11)(AX*1), Y11	// m3

	VPXOR	Y15, Y0, Y2		// ~a
	VPXOR	Y15, Y1, Y3		// ~b

	VPAND	Y0, Y1, Y4
	VPAND	Y8, Y4, Y4		// a & b & m0

	VPAND	Y0, Y3, Y5
	VPAND	Y9, Y5, Y5		// a & ~b & m1

	VPAND	Y2, Y1, Y6
	VPAND	Y10, Y6, Y6		// ~a & b & m2

	VPAND	Y2, Y3, Y7
	VPAND	Y11, Y7, Y7		// ~a & ~b & m3

	VPOR	Y4, Y5, Y4
	VPOR	Y6, Y7, Y6
	VPOR	Y4, Y6, Y4		// Y4 = result

	// Extract low byte from each of 4 uint64 lanes.
	VEXTRACTI128	$1, Y4, X5

	// Element 0
	MOVQ	X4, AX
	ANDQ	$0xFF, AX
	MOVQ	CX, R12
	SHLQ	$2, R12
	MOVQ	R12, R13
	ANDQ	$7, R13
	SHLQ	$3, R13
	XCHGQ	CX, R13
	SHLQ	CL, AX
	XCHGQ	CX, R13
	MOVQ	R12, R14
	SHRQ	$3, R14
	ORQ	AX, (DI)(R14*8)

	// Element 1
	VPEXTRQ	$1, X4, AX
	ANDQ	$0xFF, AX
	ADDQ	$1, R12
	MOVQ	R12, R13
	ANDQ	$7, R13
	SHLQ	$3, R13
	XCHGQ	CX, R13
	SHLQ	CL, AX
	XCHGQ	CX, R13
	MOVQ	R12, R14
	SHRQ	$3, R14
	ORQ	AX, (DI)(R14*8)

	// Element 2
	MOVQ	X5, AX
	ANDQ	$0xFF, AX
	ADDQ	$1, R12
	MOVQ	R12, R13
	ANDQ	$7, R13
	SHLQ	$3, R13
	XCHGQ	CX, R13
	SHLQ	CL, AX
	XCHGQ	CX, R13
	MOVQ	R12, R14
	SHRQ	$3, R14
	ORQ	AX, (DI)(R14*8)

	// Element 3
	VPEXTRQ	$1, X5, AX
	ANDQ	$0xFF, AX
	ADDQ	$1, R12
	MOVQ	R12, R13
	ANDQ	$7, R13
	SHLQ	$3, R13
	XCHGQ	CX, R13
	SHLQ	CL, AX
	XCHGQ	CX, R13
	MOVQ	R12, R14
	SHRQ	$3, R14
	ORQ	AX, (DI)(R14*8)

	INCQ	CX
	CMPQ	CX, $16
	JB	ub_avx_loop

	VZEROUPPER
	RET
