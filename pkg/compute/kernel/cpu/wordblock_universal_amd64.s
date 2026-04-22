//go:build amd64

#include "textflag.h"

// ============================================================================
// UniversalBitwise — AMD64 in-band virtual machine.
//
// Signature: func(value unsafe.Pointer)
//   value+0(FP) — pointer to a 1024-byte (128 × uint64) Value frame
//
// The function walks the program region (words 16..31), decoding each packed
// 64-bit instruction and applying the truth-table sweep in place. A zero
// instruction word terminates the program. There is no Go-side decoding, no
// allocation, no callback into managed memory. Mirrors the ARM64 NEON
// implementation in wordblock_universal_arm64.s bit-for-bit.
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
// into 8 little-endian sigWords. *All* sigBytes are computed BEFORE any
// destination writes — destinations may overlap the source regions in the
// frame, so deferred writeback is mandatory for bit-for-bit parity with the
// reference. Mode 0 XORs each sigWord into v[dstStart+j] (capped at
// min(dstSpan, 8)); mode 1 sums popcount(sigWord) across all 8 words and
// writes the total to v[dstStart].
//
// Stack frame ($128-8): 128 bytes of locals.
//   0..63   — sigBytes[64] (read back as 8 little-endian uint64 sigWords)
//   64..71  — m0
//   72..79  — m1
//   80..87  — m2
//   88..95  — m3
//   96..103 — dstSpan
//   104..111 — mode
//   112..119 — PC byte offset (128..256 step 8)
//   120..127 — j (sigWord counter, 0..7)
//
// Register map (post-decode):
//   DI  — value pointer (immutable)
//   R8,R9,R10,R11 — A lanes 0..3
//   R12 — bStart      R13 — bSpan
//   R14 — bIdx        R15 — dstStart
//   AX,BX,CX,DX,SI — scratch (CX doubles as inner-loop counter k)
// ============================================================================

TEXT ·UniversalBitwise(SB), NOSPLIT, $128-8
	MOVQ	value+0(FP), DI
	TESTQ	DI, DI
	JZ	ub_done

	MOVQ	$128, AX                // PC byte offset = 16*8
	MOVQ	AX, 112(SP)

ub_pc_loop:
	MOVQ	112(SP), SI             // SI = PC
	CMPQ	SI, $256                // 32*8
	JAE	ub_done

	MOVQ	(DI)(SI*1), AX          // AX = instruction word
	TESTQ	AX, AX
	JZ	ub_done

	// ---- decode operand fields ----
	MOVQ	AX, BX
	ANDQ	$0x7F, BX
	INCQ	BX
	MOVQ	BX, 96(SP)              // dstSpan

	MOVQ	AX, BX
	SHRQ	$7, BX
	ANDQ	$0x7F, BX
	MOVQ	BX, R15                 // dstStart

	MOVQ	AX, BX
	SHRQ	$14, BX
	ANDQ	$0x7F, BX
	INCQ	BX
	MOVQ	BX, R13                 // bSpan

	MOVQ	AX, BX
	SHRQ	$21, BX
	ANDQ	$0x7F, BX
	MOVQ	BX, R12                 // bStart

	MOVQ	AX, BX
	SHRQ	$28, BX
	ANDQ	$0x7F, BX
	INCQ	BX
	MOVQ	BX, CX                  // CX = aSpan (transient, fold counter)

	MOVQ	AX, BX
	SHRQ	$35, BX
	ANDQ	$0x7F, BX
	MOVQ	BX, DX                  // DX = aStart (transient)

	MOVQ	AX, BX
	SHRQ	$46, BX
	ANDQ	$0x1, BX
	MOVQ	BX, 104(SP)             // mode

	SHRQ	$42, AX
	ANDQ	$0xF, AX                // AX = opcode

	// ---- build masks: m_i = -((opcode >> i) & 1) ----
	MOVQ	AX, BX
	ANDQ	$1, BX
	NEGQ	BX
	MOVQ	BX, 64(SP)              // m0

	MOVQ	AX, BX
	SHRQ	$1, BX
	ANDQ	$1, BX
	NEGQ	BX
	MOVQ	BX, 72(SP)              // m1

	MOVQ	AX, BX
	SHRQ	$2, BX
	ANDQ	$1, BX
	NEGQ	BX
	MOVQ	BX, 80(SP)              // m2

	MOVQ	AX, BX
	SHRQ	$3, BX
	ANDQ	$1, BX
	NEGQ	BX
	MOVQ	BX, 88(SP)              // m3

	// ---- XOR-fold A into 4 lanes ----
	XORQ	R8, R8                  // lane 0
	XORQ	R9, R9                  // lane 1
	XORQ	R10, R10                // lane 2
	XORQ	R11, R11                // lane 3

	SHLQ	$3, DX                  // DX = aStart * 8 (byte offset)
	LEAQ	(DI)(DX*1), BX          // BX = &v[aStart]
	XORQ	SI, SI                  // SI = i counter

fold_loop:
	TESTQ	CX, CX
	JZ	fold_done
	MOVQ	(BX), AX                // AX = v[aStart + i]
	ADDQ	$8, BX

	MOVQ	SI, DX
	ANDQ	$3, DX
	TESTQ	DX, DX
	JNZ	fold_not_lane0
	XORQ	AX, R8
	JMP	fold_continue
fold_not_lane0:
	CMPQ	DX, $1
	JNE	fold_not_lane1
	XORQ	AX, R9
	JMP	fold_continue
fold_not_lane1:
	CMPQ	DX, $2
	JNE	fold_lane3
	XORQ	AX, R10
	JMP	fold_continue
fold_lane3:
	XORQ	AX, R11
fold_continue:
	INCQ	SI
	DECQ	CX
	JMP	fold_loop
fold_done:

	// ---- sweep: 8 outer (j) × 8 inner (k) = 64 sigBytes packed at 0..63(SP).
	XORQ	R14, R14                // bIdx = 0
	XORQ	AX, AX
	MOVQ	AX, 120(SP)             // j = 0

word_loop:
	MOVQ	120(SP), AX
	CMPQ	AX, $8
	JAE	writeback_phase

	XORQ	CX, CX                  // k = 0

byte_loop:
	// b = v[bStart + bIdx]
	MOVQ	R12, AX
	ADDQ	R14, AX
	MOVQ	(DI)(AX*8), DX          // DX = b

	// a = lanes[k & 3]  (branchless via CMOV)
	MOVQ	CX, AX
	ANDQ	$3, AX
	MOVQ	R8, BX                  // default: lane 0
	CMPQ	AX, $1
	CMOVQEQ	R9, BX
	CMPQ	AX, $2
	CMOVQEQ	R10, BX
	CMPQ	AX, $3
	CMOVQEQ	R11, BX                 // BX = a

	// truth table: result =
	//   (a & b) & m0
	// | (a & ~b) & m1
	// | (~a & b) & m2
	// | (~a & ~b) & m3
	MOVQ	BX, AX
	ANDQ	DX, AX
	ANDQ	64(SP), AX
	MOVQ	AX, SI                  // SI = result accumulator

	MOVQ	DX, AX
	NOTQ	AX                      // ~b
	ANDQ	BX, AX                  // a & ~b
	ANDQ	72(SP), AX
	ORQ	AX, SI

	MOVQ	BX, AX
	NOTQ	AX                      // ~a
	ANDQ	DX, AX                  // ~a & b
	ANDQ	80(SP), AX
	ORQ	AX, SI

	NOTQ	DX                      // DX = ~b (b not needed anymore)
	MOVQ	BX, AX
	NOTQ	AX                      // ~a
	ANDQ	DX, AX                  // ~a & ~b
	ANDQ	88(SP), AX
	ORQ	AX, SI                  // SI = result

	// store low byte of result at sigBytes[j*8 + k]
	MOVQ	SI, BX                  // copy result; BL = low byte
	MOVQ	120(SP), AX             // AX = j
	SHLQ	$3, AX                  // AX = j*8
	ADDQ	CX, AX                  // AX = j*8 + k = byte offset
	MOVB	BX, (SP)(AX*1)          // sigBytes[j*8 + k] = byte(result)

	// bIdx = (bIdx + 1) % bSpan
	INCQ	R14
	CMPQ	R14, R13
	JB	bidx_no_wrap
	XORQ	R14, R14
bidx_no_wrap:

	INCQ	CX
	CMPQ	CX, $8
	JL	byte_loop

	INCQ	120(SP)                 // j++
	JMP	word_loop

writeback_phase:
	MOVQ	104(SP), AX             // mode
	TESTQ	AX, AX
	JNZ	mode_reduce

	// ---- mode 0: XOR sigWord[j] into v[dstStart + j] for j in 0..min(dstSpan,8)
	MOVQ	96(SP), AX              // dstSpan
	CMPQ	AX, $8
	JBE	wb_limit_ok
	MOVQ	$8, AX
wb_limit_ok:
	MOVQ	AX, BX                  // BX = limit
	XORQ	CX, CX                  // j = 0
wb_xor_loop:
	CMPQ	CX, BX
	JAE	ub_advance_pc

	MOVQ	CX, AX
	SHLQ	$3, AX
	MOVQ	(SP)(AX*1), DX          // DX = sigWord[j]

	MOVQ	R15, AX                 // dstStart
	ADDQ	CX, AX
	XORQ	DX, (DI)(AX*8)          // v[dstStart+j] ^= sigWord[j]

	INCQ	CX
	JMP	wb_xor_loop

mode_reduce:
	// ---- mode 1: total = sum(popcount(sigWord[0..7])); v[dstStart] = total
	XORQ	DX, DX                  // total = 0
	XORQ	CX, CX                  // j = 0
wb_pop_loop:
	CMPQ	CX, $8
	JAE	wb_pop_store

	MOVQ	CX, AX
	SHLQ	$3, AX
	MOVQ	(SP)(AX*1), AX          // AX = sigWord[j]
	POPCNTQ	AX, AX
	ADDQ	AX, DX

	INCQ	CX
	JMP	wb_pop_loop
wb_pop_store:
	MOVQ	R15, AX                 // dstStart
	MOVQ	DX, (DI)(AX*8)          // v[dstStart] = total

ub_advance_pc:
	ADDQ	$8, 112(SP)             // PC += 8
	JMP	ub_pc_loop

ub_done:
	RET

