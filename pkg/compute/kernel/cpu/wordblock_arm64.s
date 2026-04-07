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


// ============================================================================
// universalBitwiseV2: reads directly from Value's pre-compiled layout.
//
// func universalBitwiseV2(value *uint64, numRotations int)
//   value+0(FP)         — pointer to 128 uint64s (the full Value)
//   numRotations+8(FP)  — number of rotations (always 16)
//
// Layout (word offsets, each word = 8 bytes):
//   [0-3]    A (query, 4 words = 32 bytes)
//   [8]      opcode (low 4 bits)
//   [16-23]  signals output (8 words = 64 bytes)
//   [32-95]  B rotations (16 rotations × 4 words, pre-compiled)
//
// Register allocation:
//   V16-V17  A pinned (2 × 128-bit = 4 words)
//   V20-V23  opcode masks m0-m3 (broadcast)
//   V28-V29  signal accumulators (zero-initialized)
//   V31      all-ones for NOT
//   V0-V15   B loads + truth table computation (4 rotations/iter)
//
// Processes 4 rotations per iteration, 4 iterations total.
// Each rotation: load 4 words of B, apply truth table against A,
// extract low byte from each result word, pack into signals.
// ============================================================================
TEXT ·universalBitwiseV2(SB), NOSPLIT|NOFRAME, $0-16
	MOVD	value+0(FP), R0

	// Pin A in V16-V17 (words 0-3, bytes 0-31).
	VLD1	(R0), [V16.B16, V17.B16]

	// Load opcode from word 8 (byte offset 64).
	MOVD	64(R0), R1
	AND	$0xF, R1, R1

	// Broadcast mask bits into V20-V23.
	// m0 = -uint64(op & 1), m1 = -uint64((op>>1)&1), etc.
	// -1 = all ones, -0 = all zeros.
	AND	$1, R1, R2
	NEG	R2, R2
	VMOV	R2, V20.D[0]
	VDUP	V20.D[0], V20.D2

	LSR	$1, R1, R3
	AND	$1, R3, R3
	NEG	R3, R3
	VMOV	R3, V21.D[0]
	VDUP	V21.D[0], V21.D2

	LSR	$2, R1, R4
	AND	$1, R4, R4
	NEG	R4, R4
	VMOV	R4, V22.D[0]
	VDUP	V22.D[0], V22.D2

	LSR	$3, R1, R5
	AND	$1, R5, R5
	NEG	R5, R5
	VMOV	R5, V23.D[0]
	VDUP	V23.D[0], V23.D2

	// V31 = all ones for NOT.
	WORD	$0x6F00E7FF		// movi v31.16b, #0xff

	// Zero signal accumulators V28-V31 → V28-V29 (8 words).
	VEOR	V28.B16, V28.B16, V28.B16
	VEOR	V29.B16, V29.B16, V29.B16

	// R1 = pointer to B rotations (word 32 = byte 256).
	ADD	$256, R0, R1

	// R2 = signal base (word 16 = byte 128).
	ADD	$128, R0, R2

	// R3 = rotation counter.
	MOVD	ZR, R3

ubv2_loop:
	// Load 4 rotations × 4 words = 16 words = 128 bytes.
	// Rotation 0: V0-V1 (4 words)
	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R1), [V1.B16]
	// Rotation 1: V2-V3
	VLD1.P	16(R1), [V2.B16]
	VLD1.P	16(R1), [V3.B16]
	// Rotation 2: V4-V5
	VLD1.P	16(R1), [V4.B16]
	VLD1.P	16(R1), [V5.B16]
	// Rotation 3: V6-V7
	VLD1.P	16(R1), [V6.B16]
	VLD1.P	16(R1), [V7.B16]

	// === Rotation 0: truth table on V0-V1 against V16-V17 ===
	// ~a
	VEOR	V16.B16, V31.B16, V8.B16	// ~a[0:1]
	VEOR	V17.B16, V31.B16, V9.B16	// ~a[2:3]
	// ~b
	VEOR	V0.B16, V31.B16, V10.B16	// ~b[0:1]
	VEOR	V1.B16, V31.B16, V11.B16	// ~b[2:3]

	// a & b & m0 (low half)
	VAND	V16.B16, V0.B16, V12.B16
	VAND	V12.B16, V20.B16, V12.B16
	// a & ~b & m1
	VAND	V16.B16, V10.B16, V13.B16
	VAND	V13.B16, V21.B16, V13.B16
	// ~a & b & m2
	VAND	V8.B16, V0.B16, V14.B16
	VAND	V14.B16, V22.B16, V14.B16
	// ~a & ~b & m3
	VAND	V8.B16, V10.B16, V15.B16
	VAND	V15.B16, V23.B16, V15.B16
	// OR all terms → V12 = result[0:1]
	VORR	V12.B16, V13.B16, V12.B16
	VORR	V14.B16, V15.B16, V14.B16
	VORR	V12.B16, V14.B16, V12.B16

	// High half (words 2-3)
	VAND	V17.B16, V1.B16, V13.B16
	VAND	V13.B16, V20.B16, V13.B16
	VAND	V17.B16, V11.B16, V14.B16
	VAND	V14.B16, V21.B16, V14.B16
	VAND	V9.B16, V1.B16, V15.B16
	VAND	V15.B16, V22.B16, V15.B16
	VAND	V9.B16, V11.B16, V0.B16
	VAND	V0.B16, V23.B16, V0.B16
	VORR	V13.B16, V14.B16, V13.B16
	VORR	V15.B16, V0.B16, V15.B16
	VORR	V13.B16, V15.B16, V13.B16
	// V12 = result words 0-1, V13 = result words 2-3

	// Extract low byte from each uint64 lane and scatter.
	// Rotation index = R3*4 + {0,1,2,3}
	LSL	$4, R3, R4		// R4 = element_base = R3*16

	// Extract 4 bytes from V12[d0], V12[d1], V13[d0], V13[d1]
	VMOV	V12.D[0], R5
	AND	$0xFF, R5, R5
	VMOV	V12.D[1], R6
	AND	$0xFF, R6, R6
	VMOV	V13.D[0], R7
	AND	$0xFF, R7, R7
	VMOV	V13.D[1], R8
	AND	$0xFF, R8, R8

	// Pack: byte[rot_base+0..3] into signal word
	// sig_word = rot_base / 8, sig_shift = (rot_base % 8) * 8
	AND	$7, R4, R9
	LSL	$3, R9, R9		// shift for byte 0
	LSL	R9, R5, R5
	ADD	$8, R9, R10
	AND	$63, R10, R10
	LSL	R10, R6, R6
	ADD	$8, R10, R11
	AND	$63, R11, R11
	LSL	R11, R7, R7
	ADD	$8, R11, R12
	AND	$63, R12, R12
	LSL	R12, R8, R8

	// Determine which signal word(s) to OR into
	LSR	$3, R4, R13		// sig_word index
	LSL	$3, R13, R14		// byte offset
	ADD	R2, R14, R14		// absolute address

	// rot_base is always 4-aligned, rot_base%8 ∈ {0,4}.
	// 4 bytes at positions [rot_base%8 .. rot_base%8+3] always
	// fit within one 8-byte signal word (max 4+3=7 < 8).
	MOVD	(R14), R15
	ORR	R5, R15, R15
	ORR	R6, R15, R15
	ORR	R7, R15, R15
	ORR	R8, R15, R15
	MOVD	R15, (R14)

	// === Rotation 1: V2-V3 against V16-V17 ===
	VEOR	V2.B16, V31.B16, V10.B16
	VEOR	V3.B16, V31.B16, V11.B16

	VAND	V16.B16, V2.B16, V12.B16
	VAND	V12.B16, V20.B16, V12.B16
	VAND	V16.B16, V10.B16, V13.B16
	VAND	V13.B16, V21.B16, V13.B16
	VAND	V8.B16, V2.B16, V14.B16
	VAND	V14.B16, V22.B16, V14.B16
	VAND	V8.B16, V10.B16, V15.B16
	VAND	V15.B16, V23.B16, V15.B16
	VORR	V12.B16, V13.B16, V12.B16
	VORR	V14.B16, V15.B16, V14.B16
	VORR	V12.B16, V14.B16, V12.B16

	VAND	V17.B16, V3.B16, V13.B16
	VAND	V13.B16, V20.B16, V13.B16
	VAND	V17.B16, V11.B16, V14.B16
	VAND	V14.B16, V21.B16, V14.B16
	VAND	V9.B16, V3.B16, V15.B16
	VAND	V15.B16, V22.B16, V15.B16
	VAND	V9.B16, V11.B16, V0.B16
	VAND	V0.B16, V23.B16, V0.B16
	VORR	V13.B16, V14.B16, V13.B16
	VORR	V15.B16, V0.B16, V15.B16
	VORR	V13.B16, V15.B16, V13.B16

	ADD	$4, R4, R4
	VMOV	V12.D[0], R5
	AND	$0xFF, R5, R5
	VMOV	V12.D[1], R6
	AND	$0xFF, R6, R6
	VMOV	V13.D[0], R7
	AND	$0xFF, R7, R7
	VMOV	V13.D[1], R8
	AND	$0xFF, R8, R8
	AND	$7, R4, R9
	LSL	$3, R9, R9
	LSL	R9, R5, R5
	ADD	$8, R9, R10
	AND	$63, R10, R10
	LSL	R10, R6, R6
	ADD	$8, R10, R11
	AND	$63, R11, R11
	LSL	R11, R7, R7
	ADD	$8, R11, R12
	AND	$63, R12, R12
	LSL	R12, R8, R8
	LSR	$3, R4, R13
	LSL	$3, R13, R14
	ADD	R2, R14, R14
	MOVD	(R14), R15
	ORR	R5, R15, R15
	ORR	R6, R15, R15
	ORR	R7, R15, R15
	ORR	R8, R15, R15
	MOVD	R15, (R14)

	// === Rotation 2: V4-V5 against V16-V17 ===
	VEOR	V4.B16, V31.B16, V10.B16
	VEOR	V5.B16, V31.B16, V11.B16

	VAND	V16.B16, V4.B16, V12.B16
	VAND	V12.B16, V20.B16, V12.B16
	VAND	V16.B16, V10.B16, V13.B16
	VAND	V13.B16, V21.B16, V13.B16
	VAND	V8.B16, V4.B16, V14.B16
	VAND	V14.B16, V22.B16, V14.B16
	VAND	V8.B16, V10.B16, V15.B16
	VAND	V15.B16, V23.B16, V15.B16
	VORR	V12.B16, V13.B16, V12.B16
	VORR	V14.B16, V15.B16, V14.B16
	VORR	V12.B16, V14.B16, V12.B16

	VAND	V17.B16, V5.B16, V13.B16
	VAND	V13.B16, V20.B16, V13.B16
	VAND	V17.B16, V11.B16, V14.B16
	VAND	V14.B16, V21.B16, V14.B16
	VAND	V9.B16, V5.B16, V15.B16
	VAND	V15.B16, V22.B16, V15.B16
	VAND	V9.B16, V11.B16, V0.B16
	VAND	V0.B16, V23.B16, V0.B16
	VORR	V13.B16, V14.B16, V13.B16
	VORR	V15.B16, V0.B16, V15.B16
	VORR	V13.B16, V15.B16, V13.B16

	ADD	$4, R4, R4
	VMOV	V12.D[0], R5
	AND	$0xFF, R5, R5
	VMOV	V12.D[1], R6
	AND	$0xFF, R6, R6
	VMOV	V13.D[0], R7
	AND	$0xFF, R7, R7
	VMOV	V13.D[1], R8
	AND	$0xFF, R8, R8
	AND	$7, R4, R9
	LSL	$3, R9, R9
	LSL	R9, R5, R5
	ADD	$8, R9, R10
	AND	$63, R10, R10
	LSL	R10, R6, R6
	ADD	$8, R10, R11
	AND	$63, R11, R11
	LSL	R11, R7, R7
	ADD	$8, R11, R12
	AND	$63, R12, R12
	LSL	R12, R8, R8
	LSR	$3, R4, R13
	LSL	$3, R13, R14
	ADD	R2, R14, R14
	MOVD	(R14), R15
	ORR	R5, R15, R15
	ORR	R6, R15, R15
	ORR	R7, R15, R15
	ORR	R8, R15, R15
	MOVD	R15, (R14)

	// === Rotation 3: V6-V7 against V16-V17 ===
	VEOR	V6.B16, V31.B16, V10.B16
	VEOR	V7.B16, V31.B16, V11.B16

	VAND	V16.B16, V6.B16, V12.B16
	VAND	V12.B16, V20.B16, V12.B16
	VAND	V16.B16, V10.B16, V13.B16
	VAND	V13.B16, V21.B16, V13.B16
	VAND	V8.B16, V6.B16, V14.B16
	VAND	V14.B16, V22.B16, V14.B16
	VAND	V8.B16, V10.B16, V15.B16
	VAND	V15.B16, V23.B16, V15.B16
	VORR	V12.B16, V13.B16, V12.B16
	VORR	V14.B16, V15.B16, V14.B16
	VORR	V12.B16, V14.B16, V12.B16

	VAND	V17.B16, V7.B16, V13.B16
	VAND	V13.B16, V20.B16, V13.B16
	VAND	V17.B16, V11.B16, V14.B16
	VAND	V14.B16, V21.B16, V14.B16
	VAND	V9.B16, V7.B16, V15.B16
	VAND	V15.B16, V22.B16, V15.B16
	VAND	V9.B16, V11.B16, V0.B16
	VAND	V0.B16, V23.B16, V0.B16
	VORR	V13.B16, V14.B16, V13.B16
	VORR	V15.B16, V0.B16, V15.B16
	VORR	V13.B16, V15.B16, V13.B16

	ADD	$4, R4, R4
	VMOV	V12.D[0], R5
	AND	$0xFF, R5, R5
	VMOV	V12.D[1], R6
	AND	$0xFF, R6, R6
	VMOV	V13.D[0], R7
	AND	$0xFF, R7, R7
	VMOV	V13.D[1], R8
	AND	$0xFF, R8, R8
	AND	$7, R4, R9
	LSL	$3, R9, R9
	LSL	R9, R5, R5
	ADD	$8, R9, R10
	AND	$63, R10, R10
	LSL	R10, R6, R6
	ADD	$8, R10, R11
	AND	$63, R11, R11
	LSL	R11, R7, R7
	ADD	$8, R11, R12
	AND	$63, R12, R12
	LSL	R12, R8, R8
	LSR	$3, R4, R13
	LSL	$3, R13, R14
	ADD	R2, R14, R14
	MOVD	(R14), R15
	ORR	R5, R15, R15
	ORR	R6, R15, R15
	ORR	R7, R15, R15
	ORR	R8, R15, R15
	MOVD	R15, (R14)

	ADD	$1, R3, R3
	CMP	$4, R3
	BLT	ubv2_loop

	RET


// ============================================================================
// batchAffinityDistances: NEON 4x-unrolled batch Hamming distance.
//
// func batchAffinityDistances(query *uint64, candidates *uint64, count int, out *uint32)
//
// Computes Hamming distance from query (8 × uint64 = 64 bytes) to each of
// count contiguous candidate vectors. Writes uint32 distances to out[i].
// 4 candidates per iteration using all 32 NEON registers.
// ============================================================================
TEXT ·batchAffinityDistances(SB), NOSPLIT|NOFRAME, $0-32
	MOVD	query+0(FP), R0
	MOVD	candidates+8(FP), R1
	MOVD	count+16(FP), R2
	MOVD	out+24(FP), R3

	CBZ	R2, bad_done

	// Load query into V16-V19
	VLD1.P	16(R0), [V16.B16]
	VLD1.P	16(R0), [V17.B16]
	VLD1.P	16(R0), [V18.B16]
	VLD1	(R0), [V19.B16]

	// R5 = count / 4, R6 = count % 4
	LSR	$2, R2, R5
	AND	$3, R2, R6

	CBZ	R5, bad_tail

bad_loop4:
	// Load 4 candidates (64 bytes each = 256 bytes total)
	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R1), [V1.B16]
	VLD1.P	16(R1), [V2.B16]
	VLD1.P	16(R1), [V3.B16]

	VLD1.P	16(R1), [V4.B16]
	VLD1.P	16(R1), [V5.B16]
	VLD1.P	16(R1), [V6.B16]
	VLD1.P	16(R1), [V7.B16]

	VLD1.P	16(R1), [V8.B16]
	VLD1.P	16(R1), [V9.B16]
	VLD1.P	16(R1), [V10.B16]
	VLD1.P	16(R1), [V11.B16]

	VLD1.P	16(R1), [V12.B16]
	VLD1.P	16(R1), [V13.B16]
	VLD1.P	16(R1), [V14.B16]
	VLD1.P	16(R1), [V15.B16]

	// XOR all with query
	VEOR	V16.B16, V0.B16, V0.B16
	VEOR	V17.B16, V1.B16, V1.B16
	VEOR	V18.B16, V2.B16, V2.B16
	VEOR	V19.B16, V3.B16, V3.B16
	VEOR	V16.B16, V4.B16, V4.B16
	VEOR	V17.B16, V5.B16, V5.B16
	VEOR	V18.B16, V6.B16, V6.B16
	VEOR	V19.B16, V7.B16, V7.B16
	VEOR	V16.B16, V8.B16, V8.B16
	VEOR	V17.B16, V9.B16, V9.B16
	VEOR	V18.B16, V10.B16, V10.B16
	VEOR	V19.B16, V11.B16, V11.B16
	VEOR	V16.B16, V12.B16, V12.B16
	VEOR	V17.B16, V13.B16, V13.B16
	VEOR	V18.B16, V14.B16, V14.B16
	VEOR	V19.B16, V15.B16, V15.B16

	// VCNT all
	VCNT	V0.B16, V0.B16
	VCNT	V1.B16, V1.B16
	VCNT	V2.B16, V2.B16
	VCNT	V3.B16, V3.B16
	VCNT	V4.B16, V4.B16
	VCNT	V5.B16, V5.B16
	VCNT	V6.B16, V6.B16
	VCNT	V7.B16, V7.B16
	VCNT	V8.B16, V8.B16
	VCNT	V9.B16, V9.B16
	VCNT	V10.B16, V10.B16
	VCNT	V11.B16, V11.B16
	VCNT	V12.B16, V12.B16
	VCNT	V13.B16, V13.B16
	VCNT	V14.B16, V14.B16
	VCNT	V15.B16, V15.B16

	// Reduce each candidate to one register
	VADD	V1.B16, V0.B16, V0.B16
	VADD	V3.B16, V2.B16, V2.B16
	VADD	V2.B16, V0.B16, V0.B16

	VADD	V5.B16, V4.B16, V4.B16
	VADD	V7.B16, V6.B16, V6.B16
	VADD	V6.B16, V4.B16, V4.B16

	VADD	V9.B16, V8.B16, V8.B16
	VADD	V11.B16, V10.B16, V10.B16
	VADD	V10.B16, V8.B16, V8.B16

	VADD	V13.B16, V12.B16, V12.B16
	VADD	V15.B16, V14.B16, V14.B16
	VADD	V14.B16, V12.B16, V12.B16

	// UADDLV + store for each candidate
	WORD	$0x6E303800    // UADDLV H0, V0.16B
	VMOV	V0.D[0], R7
	MOVW	R7, (R3)

	WORD	$0x6E303884    // UADDLV H4, V4.16B
	VMOV	V4.D[0], R7
	MOVW	R7, 4(R3)

	WORD	$0x6E303908    // UADDLV H8, V8.16B
	VMOV	V8.D[0], R7
	MOVW	R7, 8(R3)

	WORD	$0x6E30398C    // UADDLV H12, V12.16B
	VMOV	V12.D[0], R7
	MOVW	R7, 12(R3)

	ADD	$16, R3, R3
	SUB	$1, R5, R5
	CBNZ	R5, bad_loop4

bad_tail:
	CBZ	R6, bad_done

bad_loop1:
	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R1), [V1.B16]
	VLD1.P	16(R1), [V2.B16]
	VLD1.P	16(R1), [V3.B16]

	VEOR	V16.B16, V0.B16, V0.B16
	VEOR	V17.B16, V1.B16, V1.B16
	VEOR	V18.B16, V2.B16, V2.B16
	VEOR	V19.B16, V3.B16, V3.B16

	VCNT	V0.B16, V0.B16
	VCNT	V1.B16, V1.B16
	VCNT	V2.B16, V2.B16
	VCNT	V3.B16, V3.B16

	VADD	V1.B16, V0.B16, V0.B16
	VADD	V3.B16, V2.B16, V2.B16
	VADD	V2.B16, V0.B16, V0.B16

	WORD	$0x6E303800
	VMOV	V0.D[0], R7
	MOVW	R7, (R3)
	ADD	$4, R3, R3

	SUB	$1, R6, R6
	CBNZ	R6, bad_loop1

bad_done:
	RET
