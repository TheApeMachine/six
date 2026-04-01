//go:build arm64

#include "textflag.h"

// ============================================================================
// ARM64 / NEON bulk bitwise ops over []uint64 slices.
//
// All functions share the signature: func(dst, src *uint64, n int)
//   dst+0(FP)  — pointer to destination words
//   src+8(FP)  — pointer to source words
//   n+16(FP)   — number of uint64 words to process
//
// Strategy on NEON: 4× Q unroll (8 words = 64 bytes per iteration),
// then a 1× Q middle pass (2 words), then a 1× D tail (1 word).
// ============================================================================


// ----------------------------------------------------------------------------
// dst ^= src
// ----------------------------------------------------------------------------
TEXT ·simdXor(SB), NOSPLIT|NOFRAME, $0-24
	MOVD	dst+0(FP), R0
	MOVD	src+8(FP), R1
	MOVD	n+16(FP), R2

	MOVD	R2, R3
	LSR	$3, R2, R2			// n / 8
	CBZ	R2, xor_mid_setup

xor_hot:
	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R1), [V1.B16]
	VLD1.P	16(R1), [V2.B16]
	VLD1.P	16(R1), [V3.B16]

	VLD1.P	16(R0), [V4.B16]
	VLD1.P	16(R0), [V5.B16]
	VLD1.P	16(R0), [V6.B16]
	VLD1.P	16(R0), [V7.B16]

	VEOR	V0.B16, V4.B16, V0.B16
	VEOR	V1.B16, V5.B16, V1.B16
	VEOR	V2.B16, V6.B16, V2.B16
	VEOR	V3.B16, V7.B16, V3.B16

	SUB	$64, R0, R0
	VST1.P	[V0.B16], 16(R0)
	VST1.P	[V1.B16], 16(R0)
	VST1.P	[V2.B16], 16(R0)
	VST1.P	[V3.B16], 16(R0)

	SUB	$1, R2, R2
	CBNZ	R2, xor_hot

xor_mid_setup:
	AND	$7, R3, R3
	LSR	$1, R3, R2			// rem / 2
	CBZ	R2, xor_tail

xor_mid:
	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R0), [V1.B16]
	VEOR	V0.B16, V1.B16, V0.B16
	SUB	$16, R0, R0
	VST1.P	[V0.B16], 16(R0)
	SUB	$1, R2, R2
	CBNZ	R2, xor_mid

xor_tail:
	TBZ	$0, R3, xor_done
	VLD1	(R1), [V0.D1]
	VLD1	(R0), [V1.D1]
	VEOR	V0.B8, V1.B8, V0.B8
	VST1	[V0.D1], (R0)
xor_done:
	RET


// ----------------------------------------------------------------------------
// dst &= src
// ----------------------------------------------------------------------------
TEXT ·simdAnd(SB), NOSPLIT|NOFRAME, $0-24
	MOVD	dst+0(FP), R0
	MOVD	src+8(FP), R1
	MOVD	n+16(FP), R2

	MOVD	R2, R3
	LSR	$3, R2, R2
	CBZ	R2, and_mid_setup

and_hot:
	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R1), [V1.B16]
	VLD1.P	16(R1), [V2.B16]
	VLD1.P	16(R1), [V3.B16]

	VLD1.P	16(R0), [V4.B16]
	VLD1.P	16(R0), [V5.B16]
	VLD1.P	16(R0), [V6.B16]
	VLD1.P	16(R0), [V7.B16]

	VAND	V0.B16, V4.B16, V0.B16
	VAND	V1.B16, V5.B16, V1.B16
	VAND	V2.B16, V6.B16, V2.B16
	VAND	V3.B16, V7.B16, V3.B16

	SUB	$64, R0, R0
	VST1.P	[V0.B16], 16(R0)
	VST1.P	[V1.B16], 16(R0)
	VST1.P	[V2.B16], 16(R0)
	VST1.P	[V3.B16], 16(R0)

	SUB	$1, R2, R2
	CBNZ	R2, and_hot

and_mid_setup:
	AND	$7, R3, R3
	LSR	$1, R3, R2
	CBZ	R2, and_tail

and_mid:
	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R0), [V1.B16]
	VAND	V0.B16, V1.B16, V0.B16
	SUB	$16, R0, R0
	VST1.P	[V0.B16], 16(R0)
	SUB	$1, R2, R2
	CBNZ	R2, and_mid

and_tail:
	TBZ	$0, R3, and_done
	VLD1	(R1), [V0.D1]
	VLD1	(R0), [V1.D1]
	VAND	V0.B8, V1.B8, V0.B8
	VST1	[V0.D1], (R0)
and_done:
	RET


// ----------------------------------------------------------------------------
// dst |= src
// ----------------------------------------------------------------------------
TEXT ·simdOr(SB), NOSPLIT|NOFRAME, $0-24
	MOVD	dst+0(FP), R0
	MOVD	src+8(FP), R1
	MOVD	n+16(FP), R2

	MOVD	R2, R3
	LSR	$3, R2, R2
	CBZ	R2, or_mid_setup

or_hot:
	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R1), [V1.B16]
	VLD1.P	16(R1), [V2.B16]
	VLD1.P	16(R1), [V3.B16]

	VLD1.P	16(R0), [V4.B16]
	VLD1.P	16(R0), [V5.B16]
	VLD1.P	16(R0), [V6.B16]
	VLD1.P	16(R0), [V7.B16]

	VORR	V0.B16, V4.B16, V0.B16
	VORR	V1.B16, V5.B16, V1.B16
	VORR	V2.B16, V6.B16, V2.B16
	VORR	V3.B16, V7.B16, V3.B16

	SUB	$64, R0, R0
	VST1.P	[V0.B16], 16(R0)
	VST1.P	[V1.B16], 16(R0)
	VST1.P	[V2.B16], 16(R0)
	VST1.P	[V3.B16], 16(R0)

	SUB	$1, R2, R2
	CBNZ	R2, or_hot

or_mid_setup:
	AND	$7, R3, R3
	LSR	$1, R3, R2
	CBZ	R2, or_tail

or_mid:
	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R0), [V1.B16]
	VORR	V0.B16, V1.B16, V0.B16
	SUB	$16, R0, R0
	VST1.P	[V0.B16], 16(R0)
	SUB	$1, R2, R2
	CBNZ	R2, or_mid

or_tail:
	TBZ	$0, R3, or_done
	VLD1	(R1), [V0.D1]
	VLD1	(R0), [V1.D1]
	VORR	V0.B8, V1.B8, V0.B8
	VST1	[V0.D1], (R0)
or_done:
	RET


// ----------------------------------------------------------------------------
// dst = src & ~dst
// ----------------------------------------------------------------------------
TEXT ·simdSrcAndNotDst(SB), NOSPLIT|NOFRAME, $0-24
	MOVD	dst+0(FP), R0
	MOVD	src+8(FP), R1
	MOVD	n+16(FP), R2

	MOVD	R2, R3
	LSR	$3, R2, R2
	CBZ	R2, sandn_mid_setup

sandn_hot:
	VLD1.P	16(R0), [V4.B16]
	VLD1.P	16(R0), [V5.B16]
	VLD1.P	16(R0), [V6.B16]
	VLD1.P	16(R0), [V7.B16]

	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R1), [V1.B16]
	VLD1.P	16(R1), [V2.B16]
	VLD1.P	16(R1), [V3.B16]

	WORD	$0x6F00E7E8	// movi v8.16b, #0xff
	VEOR	V4.B16, V8.B16, V4.B16
	VEOR	V5.B16, V8.B16, V5.B16
	VEOR	V6.B16, V8.B16, V6.B16
	VEOR	V7.B16, V8.B16, V7.B16
	VAND	V4.B16, V0.B16, V0.B16
	VAND	V5.B16, V1.B16, V1.B16
	VAND	V6.B16, V2.B16, V2.B16
	VAND	V7.B16, V3.B16, V3.B16

	SUB	$64, R0, R0
	VST1.P	[V0.B16], 16(R0)
	VST1.P	[V1.B16], 16(R0)
	VST1.P	[V2.B16], 16(R0)
	VST1.P	[V3.B16], 16(R0)

	SUB	$1, R2, R2
	CBNZ	R2, sandn_hot

sandn_mid_setup:
	AND	$7, R3, R3
	LSR	$1, R3, R2
	CBZ	R2, sandn_tail

sandn_mid:
	VLD1.P	16(R0), [V1.B16]
	VLD1.P	16(R1), [V0.B16]
	WORD	$0x6F00E7E2	// movi v2.16b, #0xff
	VEOR	V1.B16, V2.B16, V1.B16
	VAND	V1.B16, V0.B16, V0.B16
	SUB	$16, R0, R0
	VST1.P	[V0.B16], 16(R0)
	SUB	$1, R2, R2
	CBNZ	R2, sandn_mid

sandn_tail:
	TBZ	$0, R3, sandn_done
	VLD1	(R0), [V1.D1]
	VLD1	(R1), [V0.D1]
	WORD	$0x2F00E7E2	// movi v2.8b, #0xff
	VEOR	V1.B8, V2.B8, V1.B8
	VAND	V1.B8, V0.B8, V0.B8
	VST1	[V0.D1], (R0)
sandn_done:
	RET


// ----------------------------------------------------------------------------
// dst = dst & ~src
// ----------------------------------------------------------------------------
TEXT ·simdDstAndNotSrc(SB), NOSPLIT|NOFRAME, $0-24
	MOVD	dst+0(FP), R0
	MOVD	src+8(FP), R1
	MOVD	n+16(FP), R2

	MOVD	R2, R3
	LSR	$3, R2, R2
	CBZ	R2, dandn_mid_setup

dandn_hot:
	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R1), [V1.B16]
	VLD1.P	16(R1), [V2.B16]
	VLD1.P	16(R1), [V3.B16]

	VLD1.P	16(R0), [V4.B16]
	VLD1.P	16(R0), [V5.B16]
	VLD1.P	16(R0), [V6.B16]
	VLD1.P	16(R0), [V7.B16]

	WORD	$0x6F00E7E8	// movi v8.16b, #0xff
	VEOR	V0.B16, V8.B16, V0.B16
	VEOR	V1.B16, V8.B16, V1.B16
	VEOR	V2.B16, V8.B16, V2.B16
	VEOR	V3.B16, V8.B16, V3.B16
	VAND	V0.B16, V4.B16, V0.B16
	VAND	V1.B16, V5.B16, V1.B16
	VAND	V2.B16, V6.B16, V2.B16
	VAND	V3.B16, V7.B16, V3.B16

	SUB	$64, R0, R0
	VST1.P	[V0.B16], 16(R0)
	VST1.P	[V1.B16], 16(R0)
	VST1.P	[V2.B16], 16(R0)
	VST1.P	[V3.B16], 16(R0)

	SUB	$1, R2, R2
	CBNZ	R2, dandn_hot

dandn_mid_setup:
	AND	$7, R3, R3
	LSR	$1, R3, R2
	CBZ	R2, dandn_tail

dandn_mid:
	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R0), [V1.B16]
	WORD	$0x6F00E7E2	// movi v2.16b, #0xff
	VEOR	V0.B16, V2.B16, V0.B16
	VAND	V0.B16, V1.B16, V0.B16
	SUB	$16, R0, R0
	VST1.P	[V0.B16], 16(R0)
	SUB	$1, R2, R2
	CBNZ	R2, dandn_mid

dandn_tail:
	TBZ	$0, R3, dandn_done
	VLD1	(R1), [V0.D1]
	VLD1	(R0), [V1.D1]
	WORD	$0x2F00E7E2	// movi v2.8b, #0xff
	VEOR	V0.B8, V2.B8, V0.B8
	VAND	V0.B8, V1.B8, V0.B8
	VST1	[V0.D1], (R0)
dandn_done:
	RET


// ----------------------------------------------------------------------------
// dst[i] = popcount(dst[i] ^ src[i])
//
// Reduce byte counts into per-64-bit lane counts via UADDLP:
//   B16 -> H8 -> S4 -> D2
// and similarly B8 -> H4 -> S2 -> D1 for the tail.
// ----------------------------------------------------------------------------
TEXT ·simdPopcnt(SB), NOSPLIT|NOFRAME, $0-24
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
TEXT ·simdHasHammingMatch(SB), NOSPLIT|NOFRAME, $0-33
	MOVD	frame+0(FP), R0
	MOVD	n+8(FP), R1
	MOVD	target+16(FP), R2
	MOVD	maxDist+24(FP), R3

	// Broadcast target into V20.D2, maxDist into V21.D2.
	VMOV	R2, V20.D[0]
	VDUP	V20.D[0], V20.D2
	VMOV	R3, V21.D[0]
	VDUP	V21.D[0], V21.D2

	MOVD	R1, R4
	LSR	$1, R1, R1			// n / 2
	CBZ	R1, hmatch_tail

hmatch_mid:
	VLD1.P	16(R0), [V0.B16]
	VEOR	V20.B16, V0.B16, V0.B16

	VCNT	V0.B16, V0.B16
	WORD	$0x2E203800	// uaddlp v0.8h, v0.16b
	WORD	$0x2E603800	// uaddlp v0.4s, v0.8h
	WORD	$0x2EA03800	// uaddlp v0.2d, v0.4s

	// Check both 64-bit lanes against maxDist.
	VMOV	V0.D[0], R5
	CMP	R5, R3
	BLS	hmatch_found
	VMOV	V0.D[1], R5
	CMP	R5, R3
	BLS	hmatch_found		// either lane distance <= maxDist

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

// ----------------------------------------------------------------------------
// dst[i] <<= src[i]  (x86 semantics: shift >= 64 => 0)
//
// On AArch64, USHL does per-lane variable shifts and zeros lanes for large
// counts, so this is the clean NEON equivalent.
// ----------------------------------------------------------------------------
TEXT ·simdShl(SB), NOSPLIT|NOFRAME, $0-24
	MOVD	dst+0(FP), R0
	MOVD	src+8(FP), R1
	MOVD	n+16(FP), R2

	MOVD	R2, R3
	LSR	$3, R2, R2
	CBZ	R2, shl_mid_setup

shl_hot:
	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R1), [V1.B16]
	VLD1.P	16(R1), [V2.B16]
	VLD1.P	16(R1), [V3.B16]

	VLD1.P	16(R0), [V4.B16]
	VLD1.P	16(R0), [V5.B16]
	VLD1.P	16(R0), [V6.B16]
	VLD1.P	16(R0), [V7.B16]

	WORD	$0x6EE04480	// ushl v0.2d, v4.2d, v0.2d
	WORD	$0x6EE144A1	// ushl v1.2d, v5.2d, v1.2d
	WORD	$0x6EE244C2	// ushl v2.2d, v6.2d, v2.2d
	WORD	$0x6EE344E3	// ushl v3.2d, v7.2d, v3.2d

	SUB	$64, R0, R0
	VST1.P	[V0.B16], 16(R0)
	VST1.P	[V1.B16], 16(R0)
	VST1.P	[V2.B16], 16(R0)
	VST1.P	[V3.B16], 16(R0)

	SUB	$1, R2, R2
	CBNZ	R2, shl_hot

shl_mid_setup:
	AND	$7, R3, R3
	LSR	$1, R3, R2
	CBZ	R2, shl_tail

shl_mid:
	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R0), [V1.B16]
	WORD	$0x6EE04420	// ushl v0.2d, v1.2d, v0.2d
	SUB	$16, R0, R0
	VST1.P	[V0.B16], 16(R0)
	SUB	$1, R2, R2
	CBNZ	R2, shl_mid

shl_tail:
	TBZ	$0, R3, shl_done
	VLD1	(R1), [V0.D1]
	VLD1	(R0), [V1.D1]
	WORD	$0x7EE04420	// ushl d0, d1, d0
	VST1	[V0.D1], (R0)
shl_done:
	RET


// ----------------------------------------------------------------------------
// dst[i] >>= src[i]  (x86 semantics: shift >= 64 => 0)
//
// Right shift is USHL with negated counts.
// ----------------------------------------------------------------------------
TEXT ·simdShr(SB), NOSPLIT|NOFRAME, $0-24
	MOVD	dst+0(FP), R0
	MOVD	src+8(FP), R1
	MOVD	n+16(FP), R2

	MOVD	R2, R3
	LSR	$3, R2, R2
	CBZ	R2, shr_mid_setup

shr_hot:
	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R1), [V1.B16]
	VLD1.P	16(R1), [V2.B16]
	VLD1.P	16(R1), [V3.B16]

	VLD1.P	16(R0), [V4.B16]
	VLD1.P	16(R0), [V5.B16]
	VLD1.P	16(R0), [V6.B16]
	VLD1.P	16(R0), [V7.B16]

	WORD	$0x6EE0B800	// neg v0.2d, v0.2d
	WORD	$0x6EE0B821	// neg v1.2d, v1.2d
	WORD	$0x6EE0B842	// neg v2.2d, v2.2d
	WORD	$0x6EE0B863	// neg v3.2d, v3.2d

	WORD	$0x6EE04480	// ushl v0.2d, v4.2d, v0.2d
	WORD	$0x6EE144A1	// ushl v1.2d, v5.2d, v1.2d
	WORD	$0x6EE244C2	// ushl v2.2d, v6.2d, v2.2d
	WORD	$0x6EE344E3	// ushl v3.2d, v7.2d, v3.2d

	SUB	$64, R0, R0
	VST1.P	[V0.B16], 16(R0)
	VST1.P	[V1.B16], 16(R0)
	VST1.P	[V2.B16], 16(R0)
	VST1.P	[V3.B16], 16(R0)

	SUB	$1, R2, R2
	CBNZ	R2, shr_hot

shr_mid_setup:
	AND	$7, R3, R3
	LSR	$1, R3, R2
	CBZ	R2, shr_tail

shr_mid:
	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R0), [V1.B16]
	WORD	$0x6EE0B800	// neg v0.2d, v0.2d
	WORD	$0x6EE04420	// ushl v0.2d, v1.2d, v0.2d
	SUB	$16, R0, R0
	VST1.P	[V0.B16], 16(R0)
	SUB	$1, R2, R2
	CBNZ	R2, shr_mid

shr_tail:
	TBZ	$0, R3, shr_done
	VLD1	(R1), [V0.D1]
	VLD1	(R0), [V1.D1]
	WORD	$0x7EE0B800	// neg d0, d0
	WORD	$0x7EE04420	// ushl d0, d1, d0
	VST1	[V0.D1], (R0)
shr_done:
	RET


// ----------------------------------------------------------------------------
// dst[i] = truthTable(dst[i], src[i], op)
//
// Branchless 4-bit truth table for all 16 binary Boolean opcodes.
// For each lane:
//   result = (a & b & m0) | (a & ~b & m1) | (~a & b & m2) | (~a & ~b & m3)
// where a = src[i], b = dst[i], and m0..m3 are all-ones or all-zeros masks
// derived from the 4-bit opcode.
//
// Signature: func(dst, src *uint64, n int, op uint8)
// ----------------------------------------------------------------------------
TEXT ·simdTruthTable(SB), NOSPLIT|NOFRAME, $0-25
	MOVD	dst+0(FP), R0
	MOVD	src+8(FP), R1
	MOVD	n+16(FP), R2
	MOVBU	op+24(FP), R4

	// Build scalar masks: m = -uint64(bit), i.e. 0 or 0xFFFFFFFFFFFFFFFF.
	AND	$1, R4, R5
	NEG	R5, R5
	LSR	$1, R4, R6
	AND	$1, R6, R6
	NEG	R6, R6
	LSR	$2, R4, R7
	AND	$1, R7, R7
	NEG	R7, R7
	LSR	$3, R4, R8
	AND	$1, R8, R8
	NEG	R8, R8

	// Broadcast m0..m3 into V28..V31.
	VMOV	R5, V28.D[0]
	VDUP	V28.D[0], V28.D2		// m0
	VMOV	R6, V29.D[0]
	VDUP	V29.D[0], V29.D2		// m1
	VMOV	R7, V30.D[0]
	VDUP	V30.D[0], V30.D2		// m2
	VMOV	R8, V31.D[0]
	VDUP	V31.D[0], V31.D2		// m3

	// Prepare all-ones register for NOT.
	WORD	$0x6F00E7F8			// movi v24.16b, #0xff

	MOVD	R2, R3
	LSR	$3, R2, R2
	CBZ	R2, tt_mid_setup

tt_hot:
	// Load 4x Q from src (a) and dst (b).
	VLD1.P	16(R1), [V0.B16]
	VLD1.P	16(R1), [V1.B16]
	VLD1.P	16(R1), [V2.B16]
	VLD1.P	16(R1), [V3.B16]

	VLD1.P	16(R0), [V4.B16]
	VLD1.P	16(R0), [V5.B16]
	VLD1.P	16(R0), [V6.B16]
	VLD1.P	16(R0), [V7.B16]

	// --- lane 0: a=V0, b=V4 ---
	VEOR	V0.B16, V24.B16, V8.B16	// ~a
	VEOR	V4.B16, V24.B16, V9.B16	// ~b
	VAND	V8.B16, V9.B16, V10.B16	// ~a & ~b
	VAND	V0.B16, V9.B16, V11.B16	// a & ~b
	VAND	V11.B16, V29.B16, V11.B16	// & m1
	VAND	V8.B16, V4.B16, V12.B16	// ~a & b
	VAND	V12.B16, V30.B16, V12.B16	// & m2
	VAND	V0.B16, V4.B16, V13.B16	// a & b
	VAND	V13.B16, V28.B16, V13.B16	// & m0
	VAND	V10.B16, V31.B16, V10.B16	// & m3
	VORR	V10.B16, V11.B16, V10.B16
	VORR	V12.B16, V13.B16, V12.B16
	VORR	V10.B16, V12.B16, V0.B16

	// --- lane 1: a=V1, b=V5 ---
	VEOR	V1.B16, V24.B16, V8.B16
	VEOR	V5.B16, V24.B16, V9.B16
	VAND	V8.B16, V9.B16, V10.B16
	VAND	V1.B16, V9.B16, V11.B16
	VAND	V11.B16, V29.B16, V11.B16
	VAND	V8.B16, V5.B16, V12.B16
	VAND	V12.B16, V30.B16, V12.B16
	VAND	V1.B16, V5.B16, V13.B16
	VAND	V13.B16, V28.B16, V13.B16
	VAND	V10.B16, V31.B16, V10.B16
	VORR	V10.B16, V11.B16, V10.B16
	VORR	V12.B16, V13.B16, V12.B16
	VORR	V10.B16, V12.B16, V1.B16

	// --- lane 2: a=V2, b=V6 ---
	VEOR	V2.B16, V24.B16, V8.B16
	VEOR	V6.B16, V24.B16, V9.B16
	VAND	V8.B16, V9.B16, V10.B16
	VAND	V2.B16, V9.B16, V11.B16
	VAND	V11.B16, V29.B16, V11.B16
	VAND	V8.B16, V6.B16, V12.B16
	VAND	V12.B16, V30.B16, V12.B16
	VAND	V2.B16, V6.B16, V13.B16
	VAND	V13.B16, V28.B16, V13.B16
	VAND	V10.B16, V31.B16, V10.B16
	VORR	V10.B16, V11.B16, V10.B16
	VORR	V12.B16, V13.B16, V12.B16
	VORR	V10.B16, V12.B16, V2.B16

	// --- lane 3: a=V3, b=V7 ---
	VEOR	V3.B16, V24.B16, V8.B16
	VEOR	V7.B16, V24.B16, V9.B16
	VAND	V8.B16, V9.B16, V10.B16
	VAND	V3.B16, V9.B16, V11.B16
	VAND	V11.B16, V29.B16, V11.B16
	VAND	V8.B16, V7.B16, V12.B16
	VAND	V12.B16, V30.B16, V12.B16
	VAND	V3.B16, V7.B16, V13.B16
	VAND	V13.B16, V28.B16, V13.B16
	VAND	V10.B16, V31.B16, V10.B16
	VORR	V10.B16, V11.B16, V10.B16
	VORR	V12.B16, V13.B16, V12.B16
	VORR	V10.B16, V12.B16, V3.B16

	// Store 4x Q back to dst.
	SUB	$64, R0, R0
	VST1.P	[V0.B16], 16(R0)
	VST1.P	[V1.B16], 16(R0)
	VST1.P	[V2.B16], 16(R0)
	VST1.P	[V3.B16], 16(R0)

	SUB	$1, R2, R2
	CBNZ	R2, tt_hot

tt_mid_setup:
	AND	$7, R3, R3
	LSR	$1, R3, R2
	CBZ	R2, tt_tail

tt_mid:
	VLD1.P	16(R1), [V0.B16]		// a
	VLD1.P	16(R0), [V4.B16]		// b

	VEOR	V0.B16, V24.B16, V8.B16	// ~a
	VEOR	V4.B16, V24.B16, V9.B16	// ~b
	VAND	V8.B16, V9.B16, V10.B16
	VAND	V0.B16, V9.B16, V11.B16
	VAND	V11.B16, V29.B16, V11.B16
	VAND	V8.B16, V4.B16, V12.B16
	VAND	V12.B16, V30.B16, V12.B16
	VAND	V0.B16, V4.B16, V13.B16
	VAND	V13.B16, V28.B16, V13.B16
	VAND	V10.B16, V31.B16, V10.B16
	VORR	V10.B16, V11.B16, V10.B16
	VORR	V12.B16, V13.B16, V12.B16
	VORR	V10.B16, V12.B16, V0.B16

	SUB	$16, R0, R0
	VST1.P	[V0.B16], 16(R0)
	SUB	$1, R2, R2
	CBNZ	R2, tt_mid

tt_tail:
	TBZ	$0, R3, tt_done

	VLD1	(R1), [V0.D1]			// a (upper lane zeroed)
	VLD1	(R0), [V4.D1]			// b (upper lane zeroed)

	VEOR	V0.B8, V24.B8, V8.B8	// ~a
	VEOR	V4.B8, V24.B8, V9.B8	// ~b
	VAND	V8.B8, V9.B8, V10.B8
	VAND	V0.B8, V9.B8, V11.B8
	VAND	V11.B8, V29.B8, V11.B8
	VAND	V8.B8, V4.B8, V12.B8
	VAND	V12.B8, V30.B8, V12.B8
	VAND	V0.B8, V4.B8, V13.B8
	VAND	V13.B8, V28.B8, V13.B8
	VAND	V10.B8, V31.B8, V10.B8
	VORR	V10.B8, V11.B8, V10.B8
	VORR	V12.B8, V13.B8, V12.B8
	VORR	V10.B8, V12.B8, V0.B8

	VST1	[V0.B8], (R0)
tt_done:
	RET
