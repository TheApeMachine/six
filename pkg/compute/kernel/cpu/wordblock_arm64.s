//go:build arm64

#include "textflag.h"

// ============================================================================
// ARM64 / NEON SIMD kernels for Six CPU backend.
//
// Three functions:
//   popcount            — popcount(dst[i] ^ src[i]) per word
//   hammingMatch        — early-exit Hamming distance scan
//   universalBitwise    — truth table across 64-word A×B surface
// ============================================================================


// ----------------------------------------------------------------------------
// dst[i] = popcount(dst[i] ^ src[i])
// func(dst, src *uint64, n int)
// ----------------------------------------------------------------------------
TEXT ·popcount(SB), NOSPLIT|NOFRAME, $0-24
	MOVD	dst+0(FP), R0
	MOVD	src+8(FP), R1
	MOVD	n+16(FP), R2

	MOVD	R2, R3
	LSR	$3, R2, R2
	CBZ	R2, pop_mid_setup

pop_hot:
	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R1), [V1.B16]
	VLD1.P	16(R1), [V2.B16]
	VLD1.P	16(R1), [V3.B16]

	VLD1.P	16(R0), [V4.B16]
	VLD1.P	16(R0), [V5.B16]
	VLD1.P	16(R0), [V6.B16]
	VLD1.P	16(R0), [V7.B16]

	VEOR	V4.B16, V0.B16, V0.B16
	VEOR	V5.B16, V1.B16, V1.B16
	VEOR	V6.B16, V2.B16, V2.B16
	VEOR	V7.B16, V3.B16, V3.B16

	VCNT	V0.B16, V0.B16
	VCNT	V1.B16, V1.B16
	VCNT	V2.B16, V2.B16
	VCNT	V3.B16, V3.B16

	WORD	$0x2E203800	// uaddlp v0.8h, v0.16b
	WORD	$0x2E203821	// uaddlp v1.8h, v1.16b
	WORD	$0x2E203842	// uaddlp v2.8h, v2.16b
	WORD	$0x2E203863	// uaddlp v3.8h, v3.16b

	WORD	$0x2E603800	// uaddlp v0.4s, v0.8h
	WORD	$0x2E603821	// uaddlp v1.4s, v1.8h
	WORD	$0x2E603842	// uaddlp v2.4s, v2.8h
	WORD	$0x2E603863	// uaddlp v3.4s, v3.8h

	WORD	$0x2EA03800	// uaddlp v0.2d, v0.4s
	WORD	$0x2EA03821	// uaddlp v1.2d, v1.4s
	WORD	$0x2EA03842	// uaddlp v2.2d, v2.4s
	WORD	$0x2EA03863	// uaddlp v3.2d, v3.4s

	SUB	$64, R0, R0
	VST1.P	[V0.B16], 16(R0)
	VST1.P	[V1.B16], 16(R0)
	VST1.P	[V2.B16], 16(R0)
	VST1.P	[V3.B16], 16(R0)

	SUB	$1, R2, R2
	CBNZ	R2, pop_hot

pop_mid_setup:
	AND	$7, R3, R3
	LSR	$1, R3, R2
	CBZ	R2, pop_tail

pop_mid:
	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R0), [V1.B16]
	VEOR	V1.B16, V0.B16, V0.B16
	VCNT	V0.B16, V0.B16
	WORD	$0x2E203800	// uaddlp v0.8h, v0.16b
	WORD	$0x2E603800	// uaddlp v0.4s, v0.8h
	WORD	$0x2EA03800	// uaddlp v0.2d, v0.4s
	SUB	$16, R0, R0
	VST1.P	[V0.B16], 16(R0)
	SUB	$1, R2, R2
	CBNZ	R2, pop_mid

pop_tail:
	TBZ	$0, R3, pop_done
	VLD1	(R1), [V0.D1]
	VLD1	(R0), [V1.D1]
	VEOR	V1.B8, V0.B8, V0.B8
	VCNT	V0.B8, V0.B8
	WORD	$0x0E203800	// uaddlp v0.4h, v0.8b
	WORD	$0x0E603800	// uaddlp v0.2s, v0.4h
	WORD	$0x0EA03800	// uaddlp v0.1d, v0.2s
	VST1	[V0.D1], (R0)
pop_done:
	RET


// ----------------------------------------------------------------------------
// func(frame *uint64, n int, target uint64, maxDist uint64) bool
//
// Returns true if any word has popcount(word ^ target) <= maxDist.
// ----------------------------------------------------------------------------
TEXT ·hammingMatch(SB), NOSPLIT|NOFRAME, $0-33
	MOVD	frame+0(FP), R0
	MOVD	n+8(FP), R1
	MOVD	target+16(FP), R2
	MOVD	maxDist+24(FP), R3

	VMOV	R2, V20.D[0]
	VDUP	V20.D[0], V20.D2
	VMOV	R3, V21.D[0]
	VDUP	V21.D[0], V21.D2

	MOVD	R1, R4
	LSR	$1, R1, R1
	CBZ	R1, hmatch_tail

hmatch_mid:
	VLD1.P	16(R0), [V0.B16]
	VEOR	V20.B16, V0.B16, V0.B16

	VCNT	V0.B16, V0.B16
	WORD	$0x2E203800	// uaddlp v0.8h, v0.16b
	WORD	$0x2E603800	// uaddlp v0.4s, v0.8h
	WORD	$0x2EA03800	// uaddlp v0.2d, v0.4s

	VMOV	V0.D[0], R5
	CMP	R5, R3
	BLS	hmatch_found
	VMOV	V0.D[1], R5
	CMP	R5, R3
	BLS	hmatch_found

	SUB	$1, R1, R1
	CBNZ	R1, hmatch_mid

hmatch_tail:
	TBZ	$0, R4, hmatch_not_found
	VLD1	(R0), [V0.D1]
	VEOR	V20.B8, V0.B8, V0.B8
	VCNT	V0.B8, V0.B8
	WORD	$0x0E203800	// uaddlp v0.4h, v0.8b
	WORD	$0x0E603800	// uaddlp v0.2s, v0.4h
	WORD	$0x0EA03800	// uaddlp v0.1d, v0.2s

	VMOV	V0.D[0], R5
	CMP	R5, R3
	BLS	hmatch_found

hmatch_not_found:
	MOVD	$0, R0
	MOVB	R0, ret+32(FP)
	RET

hmatch_found:
	MOVD	$1, R0
	MOVB	R0, ret+32(FP)
	RET


// ============================================================================
// universalBitwise: NEON SIMD truth table across 64-word A×B surface.
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
// Processes 2 uint64s per iteration (128 bits / Q register), 32 iterations.
// ============================================================================
TEXT ·universalBitwise(SB), NOSPLIT|NOFRAME, $0-56
	MOVD	dst+0(FP), R0
	MOVD	a+8(FP), R1
	MOVD	b+16(FP), R2
	MOVD	m0+24(FP), R3
	MOVD	m1+32(FP), R4
	MOVD	m2+40(FP), R5
	MOVD	m3+48(FP), R6

	// Zero 8 output words.
	MOVD	ZR, 0*8(R0)
	MOVD	ZR, 1*8(R0)
	MOVD	ZR, 2*8(R0)
	MOVD	ZR, 3*8(R0)
	MOVD	ZR, 4*8(R0)
	MOVD	ZR, 5*8(R0)
	MOVD	ZR, 6*8(R0)
	MOVD	ZR, 7*8(R0)

	// V31 = all ones for NOT via VEOR.
	WORD	$0x6F00E7FF		// movi v31.16b, #0xff

	// R7 = element index (0, 2, 4, ... 62).
	MOVD	ZR, R7

ub_loop:
	// Load 2 uint64s from each array.
	VLD1.P	16(R1), [V0.B16]	// a[i], a[i+1]
	VLD1.P	16(R2), [V1.B16]	// b[i], b[i+1]
	VLD1.P	16(R3), [V4.B16]	// m0
	VLD1.P	16(R4), [V5.B16]	// m1
	VLD1.P	16(R5), [V6.B16]	// m2
	VLD1.P	16(R6), [V7.B16]	// m3

	// ~a, ~b
	VEOR	V0.B16, V31.B16, V2.B16
	VEOR	V1.B16, V31.B16, V3.B16

	// a & b & m0
	VAND	V0.B16, V1.B16, V8.B16
	VAND	V8.B16, V4.B16, V8.B16

	// a & ~b & m1
	VAND	V0.B16, V3.B16, V9.B16
	VAND	V9.B16, V5.B16, V9.B16

	// ~a & b & m2
	VAND	V2.B16, V1.B16, V10.B16
	VAND	V10.B16, V6.B16, V10.B16

	// ~a & ~b & m3
	VAND	V2.B16, V3.B16, V11.B16
	VAND	V11.B16, V7.B16, V11.B16

	// OR all terms.
	VORR	V8.B16, V9.B16, V8.B16
	VORR	V10.B16, V11.B16, V10.B16
	VORR	V8.B16, V10.B16, V8.B16

	// Extract low byte of each D lane and scatter into dst.

	// Element i
	VMOV	V8.D[0], R8
	AND	$0xFF, R8, R8
	AND	$7, R7, R9
	LSL	$3, R9, R9
	LSL	R9, R8, R8
	LSR	$3, R7, R10
	LSL	$3, R10, R11
	ADD	R0, R11, R11
	MOVD	(R11), R12
	ORR	R8, R12, R12
	MOVD	R12, (R11)

	// Element i+1
	VMOV	V8.D[1], R8
	AND	$0xFF, R8, R8
	ADD	$1, R7, R13
	AND	$7, R13, R9
	LSL	$3, R9, R9
	LSL	R9, R8, R8
	LSR	$3, R13, R10
	LSL	$3, R10, R11
	ADD	R0, R11, R11
	MOVD	(R11), R12
	ORR	R8, R12, R12
	MOVD	R12, (R11)

	ADD	$2, R7, R7
	CMP	$64, R7
	BLT	ub_loop

	RET
