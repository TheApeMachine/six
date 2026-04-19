//go:build arm64

#include "textflag.h"

// ============================================================================
// UniversalBitwise — ARM64 in-band virtual machine.
//
// Signature: func(value unsafe.Pointer)
//   value+0(FP) — pointer to a 1024-byte (128 × uint64) Value frame
//
// The function walks the program region (words 16..31), decoding each packed
// 64-bit instruction and applying the truth-table sweep in place. A zero
// instruction word terminates the program. There is no Go-side decoding, no
// allocation, no callback into managed memory.
//
// Instruction layout (matches pkg/compute/program.EncodeInstruction):
//   bits  0..6   dstSpan - 1
//   bits  7..13  dstStart
//   bits 14..20  bSpan - 1
//   bits 21..27  bStart
//   bits 28..34  aSpan - 1
//   bits 35..41  aStart
//   bits 42..45  opcode (4-bit truth table)
//   bits 46      mode (0=accumulate XOR, 1=reduce popcount)
//
// Sweep semantics: XOR-fold the A region into 4 lanes (lane[i mod 4] ^=
// v[aStart+i]), then for sigByte index i in 0..63 compute the truth-table
// result of (a=lane[i mod 4], b=v[bStart + (i mod bSpan)]) using the opcode
// nibble's 4 bits as minterm enables m0..m3, take the low byte, and pack
// into 8 little-endian sigWords. *All* sigWords are computed BEFORE any
// destination writes — destinations may overlap the source regions in the
// frame, so deferred writeback is mandatory for bit-for-bit parity with the
// reference. Mode 0 XORs each sigWord into v[dstStart+j] (capped at
// min(dstSpan, 8)); mode 1 sums popcount(sigWord) across all 8 words and
// writes the total to v[dstStart].
//
// Stack frame: 64 bytes of scratch hold the 8 staged sigWords at offsets
// 0..56 from RSP. Go inserts the standard FP/LR save above this region.
//
// Register map (post-decode):
//   R0  — value pointer (immutable)
//   R1  — PC byte offset (128..256 step 8)
//   R4  — dstStart            R5  — dstSpan
//   R6  — bStart              R7  — bSpan
//   R10 — j (sigWord counter, 0..7) — reused after opcode is consumed
//   R11 — mode (0/1)
//   R16,R17,R19,R20 — A lanes (R18 is reserved on darwin/arm64)
//   R21..R24 — m0..m3 (scalar all-zero or all-ones masks)
//   R25 — total (mode 1 popcount accumulator)
//   R26 — bIdx (running B offset, 0..bSpan-1)
//   R27 — current sigWord accumulator (in-flight word)
//   R2,R3,R8,R9,R12,R13,R14,R15 — scratch
// ============================================================================

TEXT ·UniversalBitwise(SB), NOSPLIT, $64-8
	MOVD	value+0(FP), R0
	CBZ	R0, ub_done

	MOVD	$128, R1                // PC byte offset = 16*8

ub_pc_loop:
	CMP	$256, R1                // 32*8 = 256
	BGE	ub_done

	ADD	R0, R1, R8
	MOVD	(R8), R2                // R2 = instruction word
	CBZ	R2, ub_done

	// ---- decode operand fields ----
	AND	$0x7F, R2, R5
	ADD	$1, R5, R5              // R5 = dstSpan (1..128)

	LSR	$7, R2, R3
	AND	$0x7F, R3, R4           // R4 = dstStart

	LSR	$14, R2, R3
	AND	$0x7F, R3, R7
	ADD	$1, R7, R7              // R7 = bSpan

	LSR	$21, R2, R3
	AND	$0x7F, R3, R6           // R6 = bStart

	LSR	$28, R2, R3
	AND	$0x7F, R3, R9
	ADD	$1, R9, R9              // R9 = aSpan

	LSR	$35, R2, R3
	AND	$0x7F, R3, R8           // R8 = aStart

	LSR	$42, R2, R12
	AND	$0xF, R12, R12          // R12 = opcode (temp)

	LSR	$46, R2, R11
	AND	$0x1, R11, R11          // R11 = mode

	// ---- build masks: m_i = -((opcode >> i) & 1) → 0 or all-ones ----
	AND	$1, R12, R3
	NEG	R3, R21                 // R21 = m0

	LSR	$1, R12, R3
	AND	$1, R3, R3
	NEG	R3, R22                 // R22 = m1

	LSR	$2, R12, R3
	AND	$1, R3, R3
	NEG	R3, R23                 // R23 = m2

	LSR	$3, R12, R3
	AND	$1, R3, R3
	NEG	R3, R24                 // R24 = m3

	// ---- XOR-fold A into 4 lanes (R18 is reserved on darwin) ----
	MOVD	ZR, R16                 // lane 0
	MOVD	ZR, R17                 // lane 1
	MOVD	ZR, R19                 // lane 2
	MOVD	ZR, R20                 // lane 3

	LSL	$3, R8, R12
	ADD	R0, R12, R12            // R12 = &v[aStart]
	MOVD	R9, R13                 // R13 = aSpan remaining
	MOVD	ZR, R14                 // R14 = i

fold_loop:
	CBZ	R13, fold_done
	MOVD	(R12), R15
	ADD	$8, R12, R12

	AND	$3, R14, R3
	TBNZ	$1, R3, fold_lane23
	TBNZ	$0, R3, fold_lane1
	EOR	R15, R16, R16           // lane 0
	JMP	fold_continue
fold_lane1:
	EOR	R15, R17, R17           // lane 1
	JMP	fold_continue
fold_lane23:
	TBNZ	$0, R3, fold_lane3
	EOR	R15, R19, R19           // lane 2
	JMP	fold_continue
fold_lane3:
	EOR	R15, R20, R20           // lane 3
fold_continue:
	ADD	$1, R14, R14
	SUB	$1, R13, R13
	JMP	fold_loop
fold_done:

	// ---- sweep: 8 outer × 8 inner = 64 sigBytes packed into 8 sigWords.
	// Stage each sigWord into the 64-byte stack scratch at offset j*8.
	MOVD	ZR, R10                 // j = 0
	MOVD	ZR, R26                 // bIdx = 0

word_loop:
	CMP	$8, R10
	BGE	writeback_phase

	MOVD	ZR, R27                 // sigWord accumulator = 0
	MOVD	ZR, R12                 // k = 0 (byte index within word)

byte_loop:
	// b = v[bStart + bIdx]
	ADD	R6, R26, R3
	LSL	$3, R3, R3
	ADD	R0, R3, R3
	MOVD	(R3), R13               // R13 = b

	// pick a = lanes[k & 3] into R3
	AND	$3, R12, R8
	TBNZ	$1, R8, byte_lane23
	TBNZ	$0, R8, byte_lane1
	MOVD	R16, R3
	JMP	byte_have_a
byte_lane1:
	MOVD	R17, R3
	JMP	byte_have_a
byte_lane23:
	TBNZ	$0, R8, byte_lane3
	MOVD	R19, R3
	JMP	byte_have_a
byte_lane3:
	MOVD	R20, R3
byte_have_a:

	// truth table:
	//   R14 = (a & b) & m0
	//   R8  = (a & ~b) & m1
	//   R15 = (~a & b) & m2
	//   R2  = (~a & ~b) & m3
	AND	R13, R3, R14
	AND	R21, R14, R14

	MVN	R13, R8                 // R8 = ~b
	AND	R3, R8, R8              // R8 = a & ~b
	AND	R22, R8, R8             // R8 = a & ~b & m1

	MVN	R3, R9                  // R9 = ~a
	AND	R13, R9, R15            // R15 = ~a & b
	AND	R23, R15, R15           // R15 = ~a & b & m2

	MVN	R13, R2                 // R2 = ~b (recompute)
	AND	R9, R2, R2              // R2 = ~a & ~b
	AND	R24, R2, R2             // R2 = ~a & ~b & m3

	ORR	R8, R14, R14
	ORR	R2, R15, R15
	ORR	R15, R14, R14           // R14 = result

	// pack low byte into R27 at position (k & 7) * 8
	AND	$0xFF, R14, R14
	AND	$7, R12, R8
	LSL	$3, R8, R8
	LSL	R8, R14, R14
	ORR	R14, R27, R27

	// bIdx = (bIdx + 1) % bSpan
	ADD	$1, R26, R26
	CMP	R7, R26
	BLO	bidx_no_wrap
	MOVD	ZR, R26
bidx_no_wrap:

	ADD	$1, R12, R12
	CMP	$8, R12
	BLT	byte_loop

	// stage sigWord[j] at scratch offset j*8 (RSP-relative).
	LSL	$3, R10, R3
	ADD	R3, RSP, R3
	MOVD	R27, (R3)

	ADD	$1, R10, R10
	JMP	word_loop

writeback_phase:
	CBNZ	R11, mode_reduce

	// ---- mode 0: XOR scratch[j] into v[dstStart + j] for j in 0..min(dstSpan,8)
	MOVD	$8, R8                  // R8 = limit = min(dstSpan, 8)
	CMP	R5, R8
	BLO	wb_limit_set            // if 8 < dstSpan, keep 8
	MOVD	R5, R8
wb_limit_set:
	MOVD	ZR, R10                 // j = 0
wb_xor_loop:
	CMP	R8, R10
	BGE	ub_advance_pc
	LSL	$3, R10, R3
	ADD	R3, RSP, R3
	MOVD	(R3), R12               // R12 = sigWord[j]
	ADD	R4, R10, R3
	LSL	$3, R3, R3
	ADD	R0, R3, R3
	MOVD	(R3), R9
	EOR	R12, R9, R9
	MOVD	R9, (R3)
	ADD	$1, R10, R10
	JMP	wb_xor_loop

mode_reduce:
	// ---- mode 1: total = sum(popcount(scratch[0..7])); v[dstStart] = total
	MOVD	ZR, R25                 // total = 0
	MOVD	ZR, R10                 // j = 0
wb_pop_loop:
	CMP	$8, R10
	BGE	wb_pop_store
	LSL	$3, R10, R3
	ADD	R3, RSP, R3
	MOVD	(R3), R12               // sigWord[j]
	VMOV	R12, V0.D[0]
	VCNT	V0.B8, V0.B8
	WORD	$0x2E303800             // UADDLV H0, V0.8B (unsigned)
	VMOV	V0.B[0], R3
	ADD	R3, R25, R25
	ADD	$1, R10, R10
	JMP	wb_pop_loop
wb_pop_store:
	LSL	$3, R4, R3
	ADD	R0, R3, R3
	MOVD	R25, (R3)

ub_advance_pc:
	ADD	$8, R1, R1
	JMP	ub_pc_loop

ub_done:
	RET
