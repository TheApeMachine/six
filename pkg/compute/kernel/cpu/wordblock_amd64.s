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
// Staged companion to universalBitwiseSweep in wordblock_universal.go. The wire
// format for each 4-bit opcode is value.opcodes in cmd/cfg/config.yml (same
// nibbles as pkg/compute/firmware.OperationType): interpret the four
// characters as binary with the rightmost digit = LSB. Bit k of the nibble
// selects whether minterm k contributes (caller broadcasts 0 or ~0 per lane):
//   op&1 → m0 / (a ∧ b)
//   op&2 → m1 / (a ∧ ¬b)
//   op&4 → m2 / (¬a ∧ b)
//   op&8 → m3 / (¬a ∧ ¬b)
// Result is (a∧b∧m0)|(a∧¬b∧m1)|(¬a∧b∧m2)|(¬a∧¬b∧m3). Signature bytes are
// packed into dst the same way as the Go reference (64 bytes → 8× little-endian
// uint64). value.num_rotations is 16; CX counts rotation groups 0..15 (four
// lanes each).
//
// Signature: func(dst, a, b, m0, m1, m2, m3 *uint64)
//   dst+0(FP)  — pointer to 8 uint64s (output signals)
//   a+8(FP)    — pointer to 64 uint64s (A surface)
//   b+16(FP)   — pointer to 64 uint64s (B surface)
//   m0+24(FP)  — pointer to 64 uint64s (all-zero or all-ones lanes for m0)
//   m1+32(FP)  — pointer to 64 uint64s (m1)
//   m2+40(FP)  — pointer to 64 uint64s (m2)
//   m3+48(FP)  — pointer to 64 uint64s (m3)
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


// =========================================================================
// batchAffinityDistances: AVX2 2x-unrolled batch Hamming distance.
//
// func batchAffinityDistances(query *uint64, candidates *uint64, count int, out *uint32)
//
// Computes Hamming distance from query (8 × uint64 = 64 bytes) to each of
// count contiguous candidate vectors. Writes uint32 distances to out[i].
// 2 candidates per iteration for ILP across execution ports.
// =========================================================================
TEXT ·batchAffinityDistances(SB), NOSPLIT, $0-32
	MOVQ	query+0(FP), DI
	MOVQ	candidates+8(FP), SI
	MOVQ	count+16(FP), CX
	MOVQ	out+24(FP), R10

	TESTQ	CX, CX
	JZ	bad_done

	VMOVDQU	popcnt_lut<>(SB), Y14
	VMOVDQU	mask_0f<>(SB), Y13
	VPXOR	Y15, Y15, Y15

	VMOVDQU	0(DI), Y10
	VMOVDQU	32(DI), Y11

	// R11 = count / 2, R12 = count % 2
	MOVQ	CX, R11
	SHRQ	$1, R11
	MOVQ	CX, R12
	ANDQ	$1, R12

	TESTQ	R11, R11
	JZ	bad_tail

bad_loop2:
	// Candidate A
	VMOVDQU	0(SI), Y0
	VMOVDQU	32(SI), Y1
	// Candidate B
	VMOVDQU	64(SI), Y4
	VMOVDQU	96(SI), Y5

	VPXOR	Y10, Y0, Y0
	VPXOR	Y11, Y1, Y1
	VPXOR	Y10, Y4, Y4
	VPXOR	Y11, Y5, Y5

	// Popcount A: Y0
	VMOVDQU	Y0, Y2
	VPSRLW	$4, Y2, Y2
	VPAND	Y13, Y2, Y2
	VPAND	Y13, Y0, Y0
	VPSHUFB	Y2, Y14, Y2
	VPSHUFB	Y0, Y14, Y0
	VPADDB	Y2, Y0, Y0
	VPSADBW	Y15, Y0, Y0

	// Popcount A: Y1
	VMOVDQU	Y1, Y3
	VPSRLW	$4, Y3, Y3
	VPAND	Y13, Y3, Y3
	VPAND	Y13, Y1, Y1
	VPSHUFB	Y3, Y14, Y3
	VPSHUFB	Y1, Y14, Y1
	VPADDB	Y3, Y1, Y1
	VPSADBW	Y15, Y1, Y1

	// Popcount B: Y4
	VMOVDQU	Y4, Y6
	VPSRLW	$4, Y6, Y6
	VPAND	Y13, Y6, Y6
	VPAND	Y13, Y4, Y4
	VPSHUFB	Y6, Y14, Y6
	VPSHUFB	Y4, Y14, Y4
	VPADDB	Y6, Y4, Y4
	VPSADBW	Y15, Y4, Y4

	// Popcount B: Y5
	VMOVDQU	Y5, Y7
	VPSRLW	$4, Y7, Y7
	VPAND	Y13, Y7, Y7
	VPAND	Y13, Y5, Y5
	VPSHUFB	Y7, Y14, Y7
	VPSHUFB	Y5, Y14, Y5
	VPADDB	Y7, Y5, Y5
	VPSADBW	Y15, Y5, Y5

	// Horizontal sum A
	VPADDQ	Y1, Y0, Y0
	VEXTRACTI128	$1, Y0, X1
	VPADDQ	X1, X0, X0
	VMOVQ	X0, AX
	MOVL	AX, (R10)

	// Horizontal sum B
	VPADDQ	Y5, Y4, Y4
	VEXTRACTI128	$1, Y4, X5
	VPADDQ	X5, X4, X4
	VMOVQ	X4, AX
	MOVL	AX, 4(R10)

	ADDQ	$8, R10
	ADDQ	$128, SI
	DECQ	R11
	JNZ	bad_loop2

bad_tail:
	TESTQ	R12, R12
	JZ	bad_done

	VMOVDQU	0(SI), Y0
	VMOVDQU	32(SI), Y1

	VPXOR	Y10, Y0, Y0
	VPXOR	Y11, Y1, Y1

	VMOVDQU	Y0, Y2
	VPSRLW	$4, Y2, Y2
	VPAND	Y13, Y2, Y2
	VPAND	Y13, Y0, Y0
	VPSHUFB	Y2, Y14, Y2
	VPSHUFB	Y0, Y14, Y0
	VPADDB	Y2, Y0, Y0
	VPSADBW	Y15, Y0, Y0

	VMOVDQU	Y1, Y3
	VPSRLW	$4, Y3, Y3
	VPAND	Y13, Y3, Y3
	VPAND	Y13, Y1, Y1
	VPSHUFB	Y3, Y14, Y3
	VPSHUFB	Y1, Y14, Y1
	VPADDB	Y3, Y1, Y1
	VPSADBW	Y15, Y1, Y1

	VPADDQ	Y1, Y0, Y0
	VEXTRACTI128	$1, Y0, X1
	VPADDQ	X1, X0, X0
	VMOVQ	X0, AX
	MOVL	AX, (R10)

bad_done:
	VZEROUPPER
	RET

// affinityPopcount: popcount of a 5-word (257-bit) affinity vector.
// Five scalar POPCNT instructions is dramatically cheaper than spinning
// up an AVX2 shuffle-LUT pipeline for a single 40-byte payload. The
// fifth word is masked to bit 0 to honour AffinityLastWordMask.
//
// func affinityPopcount(vec *uint64) uint32
TEXT ·affinityPopcount(SB), NOSPLIT|NOFRAME, $0-12
	MOVQ	vec+0(FP), SI
	POPCNTQ	(SI), AX
	POPCNTQ	8(SI), CX
	ADDQ	CX, AX
	POPCNTQ	16(SI), CX
	ADDQ	CX, AX
	POPCNTQ	24(SI), CX
	ADDQ	CX, AX
	MOVQ	32(SI), CX
	ANDQ	$1, CX
	ADDQ	CX, AX
	MOVL	AX, ret+8(FP)
	RET

// affinityCoupling: fused Jaccard numerator (popcount(a & b)) and
// denominator (popcount(a | b)) across two 5-word affinity vectors.
// Scalar AND/OR/POPCNT per lane; keeping the work in GPRs avoids any
// SSE/AVX state transition on the hot DHT routing path.
//
// func affinityCoupling(a *uint64, b *uint64, out *uint32)
TEXT ·affinityCoupling(SB), NOSPLIT|NOFRAME, $0-24
	MOVQ	a+0(FP), SI
	MOVQ	b+8(FP), DI
	MOVQ	out+16(FP), R8

	XORQ	AX, AX			// intersection accumulator
	XORQ	DX, DX			// union accumulator

	MOVQ	(SI), R9
	MOVQ	(DI), R10
	MOVQ	R9, R11
	ANDQ	R10, R11
	POPCNTQ	R11, R11
	ADDQ	R11, AX
	ORQ	R10, R9
	POPCNTQ	R9, R9
	ADDQ	R9, DX

	MOVQ	8(SI), R9
	MOVQ	8(DI), R10
	MOVQ	R9, R11
	ANDQ	R10, R11
	POPCNTQ	R11, R11
	ADDQ	R11, AX
	ORQ	R10, R9
	POPCNTQ	R9, R9
	ADDQ	R9, DX

	MOVQ	16(SI), R9
	MOVQ	16(DI), R10
	MOVQ	R9, R11
	ANDQ	R10, R11
	POPCNTQ	R11, R11
	ADDQ	R11, AX
	ORQ	R10, R9
	POPCNTQ	R9, R9
	ADDQ	R9, DX

	MOVQ	24(SI), R9
	MOVQ	24(DI), R10
	MOVQ	R9, R11
	ANDQ	R10, R11
	POPCNTQ	R11, R11
	ADDQ	R11, AX
	ORQ	R10, R9
	POPCNTQ	R9, R9
	ADDQ	R9, DX

	MOVQ	32(SI), R9
	MOVQ	32(DI), R10
	ANDQ	$1, R9
	ANDQ	$1, R10
	MOVQ	R9, R11
	ANDQ	R10, R11
	ADDQ	R11, AX
	ORQ	R10, R9
	ADDQ	R9, DX

	MOVL	AX, (R8)
	MOVL	DX, 4(R8)
	RET
