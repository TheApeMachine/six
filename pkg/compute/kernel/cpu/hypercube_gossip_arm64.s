//go:build arm64
#include "textflag.h"

TEXT ·executeKernel(SB), NOSPLIT, $192-80
	MOVD ownerFrame+8(FP), R19
	MOVD $0, R20 // pc
	MOVD $0, R21 // bQueueIdx
	// Persistent state across pc-loop iterations:
	// 128(RSP) currentB    — peer the most recent pop seed bound
	// 136(RSP) popBodyStart — pc to rewind to when popEnd fires
	// 144(RSP) popActive   — 1 between pop seed and lane drain
	// 152(RSP) popEnd      — bit 63 of the current instruction
	MOVD ZR, 128(RSP)
	MOVD ZR, 136(RSP)
	MOVD ZR, 144(RSP)
	MOVD ZR, 152(RSP)

pc_loop:
	CMP $16, R20
	BGE end_pc_loop

	// instr = ownerFrame[16+pc]
	ADD $16, R20, R0
	LSL $3, R0
	ADD R19, R0
	MOVD (R0), R22

	CBZ R22, next_pc

	// --- 1. DECODE INSTRUCTION ---
	AND $0xF, R22, R0 // opcode
	
	// Truthtable combinations
	AND $1, R0, R1; NEG R1, R1; MOVD R1, 56(RSP) // m0
	LSR $1, R0, R1; AND $1, R1; NEG R1, R1; MOVD R1, 64(RSP) // m1
	LSR $2, R0, R1; AND $1, R1; NEG R1, R1; MOVD R1, 72(RSP) // m2
	LSR $3, R0, R1; AND $1, R1; NEG R1, R1; MOVD R1, 80(RSP) // m3

	// UBFX (Extract Bitfield: LSB, WIDTH, SRC, DST)
	UBFX $4, R22, $7, R0; MOVD R0, 0(RSP) // aStart
	UBFX $11, R22, $7, R0; ADD $1, R0; MOVD R0, 8(RSP) // aSpan
	UBFX $18, R22, $7, R0; MOVD R0, 16(RSP) // bStart
	UBFX $25, R22, $7, R0; ADD $1, R0; MOVD R0, 24(RSP) // bSpan
	UBFX $32, R22, $7, R0; MOVD R0, 32(RSP) // dstStart
	UBFX $39, R22, $7, R0; ADD $1, R0; MOVD R0, 40(RSP) // dstSpan
	
	// mask = ownerFrame[maskStart], also stash maskStart for per-peer
	// reads from peer frames (e.g. TopoHypercubePerPeer stage check).
	UBFX $46, R22, $7, R0
	MOVD R0, 184(RSP)
	LSL $3, R0
	ADD R19, R0, R1
	MOVD (R1), R1
	MOVD R1, 48(RSP)

	UBFX $54, R22, $1, R0; MOVD R0, 112(RSP) // emit
	UBFX $63, R22, $1, R0; MOVD R0, 152(RSP) // popEnd
	UBFX $57, R22, $1, R0; MOVD R0, 160(RSP) // predicate
	UBFX $58, R22, $3, R0; MOVD R0, 168(RSP) // predCond
	UBFX $62, R22, $1, R0; MOVD R0, 176(RSP) // stageBit
	UBFX $55, R22, $2, R0 // topology

	// --- 2. TOPOLOGY ROUTING ---
	// Default ptrB to the persistent currentB (set by the most recent
	// pop seed). Gossip / pop-seed branches override; if no seed has
	// fired yet the slot is nil and the failsafe in topo_done routes
	// the read back at the owner frame.
	MOVD 128(RSP), R23

	CMP $1, R0; BEQ topo_pop
	CMP $2, R0; BEQ topo_hyper
	B topo_done

topo_pop:
	MOVD communitySize+48(FP), R1
	CMP R1, R21; BHS topo_done // if bQueueIdx >= communitySize
	MOVD community_ptr+24(FP), R2
	MOVD (R2)(R21<<3), R23
	MOVD R23, 128(RSP) // persist currentB so subsequent pop body
	                   // instructions in the same body read this peer.
	ADD $1, R21
	// popBodyStart = pc + 1; popActive = 1. The pop seed instruction
	// stages the body that follows, and popEnd at the end of the body
	// rewinds back here so each peer in the lane gets a body sweep.
	ADD $1, R20, R3
	MOVD R3, 136(RSP)
	MOVD $1, R3
	MOVD R3, 144(RSP)
	B topo_done

topo_hyper:
	MOVD dimCount+56(FP), R1
	CBZ R1, topo_done

	// pc % dimCount
	UDIV R1, R20, R2
	MSUB R2, R1, R20, R3 // R3 = pc % dimCount
	
	MOVD $1, R4
	LSL R3, R4
	MOVD ownerIdx+16(FP), R5
	EOR R4, R5, R6 // R6 = peerIdx

	MOVD communitySize+48(FP), R1
	CMP R1, R6; BHS topo_fallback
	MOVD community_ptr+24(FP), R2
	MOVD (R2)(R6<<3), R23
	B topo_done
topo_fallback:
	MOVD $0, R23

topo_done:
	CBNZ R23, skip_ptrb_fix
	MOVD R19, R23 // failsafe
skip_ptrb_fix:
	MOVD R23, 96(RSP)

	// stage(B) dispatch. stage instructions skip the truth-table body
	// but still honor pop-end rewinding, so stage_path jumps to
	// popend_check after recording the staged peer index.
	MOVD 176(RSP), R0
	CBZ R0, no_stage
	B stage_path
no_stage:

	// srcAFromB routing: bit 61 makes ptrA point at the bound peer
	// instead of the owner, so `write B.x <- xor(B.x, B.y)` works
	// without reverse-engineering the operand routing.
	UBFX $61, R22, $1, R0
	CMP $1, R0; BNE ptra_owner
	MOVD R23, 88(RSP)
	B ptra_done
ptra_owner:
	MOVD R19, 88(RSP)
ptra_done:

	// Target B pointer routing
	UBFX $53, R22, $1, R0
	MOVD R19, R24 // ptrDst = ownerFrame
	CMP $1, R0; BNE ptrdst_ok
	MOVD R23, R24 // ptrDst = ptrB
ptrdst_ok:
	MOVD R24, 104(RSP)

	// Predicate dispatch. When the predicate bit is set the kernel
	// computes a popcount-based mask (or stores the popcount itself
	// per StorePopcnt / AnyZero) and skips the truth-table broadcast.
	MOVD 160(RSP), R0
	CBZ R0, no_predicate
	B predicate_path
no_predicate:

	// --- 2b. PER-PEER HYPERCUBE BROADCAST ROUTING ---
	// When topology == 2 (Hypercube) AND target == B, the new ALU
	// writes the truth-table result to EVERY peer's dst on the same
	// instruction (broadcast), not just one. The single-pass inner
	// loop below handles the non-broadcast cases; this block handles
	// the broadcast by re-pointing ptrB / ptrDst at each peer in turn
	// and re-running the truth-table body. emit / pop-end check below
	// fires once after all peers are done, matching the Go reference.
	UBFX $55, R22, $2, R25 // topology
	UBFX $53, R22, $1, R26 // targetB
	CMP $2, R25; BEQ bcast_check_target_arm64
	CMP $3, R25; BEQ bcast_check_target_arm64
	B single_pass
bcast_check_target_arm64:
	CMP $1, R26; BNE single_pass

	// Per-peer broadcast: loop k = 0 .. communitySize-1, skip ownerIdx.
	// Note: avoid R28 (Go's runtime g register on arm64) — load ownerIdx
	// fresh each iteration into R0 instead of pinning it to a register.
	MOVD $0, R25                 // peer_k
	MOVD communitySize+48(FP), R26
	MOVD community_ptr+24(FP), R27
bcast_peer_loop:
	CMP R26, R25; BHS bcast_done
	MOVD ownerIdx+16(FP), R0
	CMP R0, R25; BEQ bcast_skip_self
	MOVD (R27)(R25<<3), R23      // peer = community[k]
	CBZ R23, bcast_skip_self     // skip nil peers (defensive parity with Go)
	MOVD R23, 96(RSP)            // ptrB = peer
	MOVD R23, 104(RSP)           // ptrDst = peer

	// Per-peer mask: TopoHypercubePerPeer (topology==3) reads the mask
	// from peer[maskStart] each iteration so a preceding per-peer
	// predicate can gate the broadcast write per peer. TopoHypercube
	// (topology==2) keeps the owner-side mask. R17 holds the active
	// mask through the inlined inner loop below.
	UBFX $55, R22, $2, R0
	CMP $3, R0; BNE bc_owner_mask
	MOVD 184(RSP), R5
	AND $127, R5
	MOVD (R23)(R5<<3), R17
	B bc_mask_set
bc_owner_mask:
	MOVD 48(RSP), R17
bc_mask_set:

	// Inlined inner truth-table loop, identical body to inner_loop
	// below, mutating peer's dst rather than the owner's. Kept inline
	// (not as a labeled subroutine) to avoid clobbering the link
	// register inside this NOSPLIT frame.
	MOVD $0, R4
	MOVD 0(RSP), R5;  MOVD $0, R6
	MOVD 16(RSP), R7; MOVD $0, R8
	MOVD 32(RSP), R9
bcast_inner_loop:
	MOVD 40(RSP), R10
	CMP R10, R4; BHS bcast_inner_done

	AND $127, R5, R1
	MOVD 88(RSP), R2
	MOVD (R2)(R1<<3), R11
	AND $127, R7, R1
	MOVD 96(RSP), R2
	MOVD (R2)(R1<<3), R12

	// bRotate (broadcast variant). Same logic as sp_no_rot above; the
	// broadcast block uses bcast_* labels to keep its inline copy of
	// the inner loop independent.
	UBFX $58, R22, $3, R0
	CBZ R0, bc_no_rot
	ADD $1, R8, R3
	MOVD 24(RSP), R16
	CMP R16, R3; BLO bc_have_next
	MOVD $0, R3
bc_have_next:
	MOVD 16(RSP), R16
	ADD R16, R3
	AND $127, R3
	MOVD 96(RSP), R16
	MOVD (R16)(R3<<3), R3
	LSL $3, R0
	LSR R0, R12, R12
	MOVD $64, R16
	SUB R0, R16
	LSL R16, R3
	ORR R3, R12
bc_no_rot:

	MOVD 56(RSP), R13
	AND R12, R11, R1; AND R13, R1, R1
	MOVD 64(RSP), R13
	BIC R12, R11, R2; AND R13, R2, R2
	MOVD 72(RSP), R13
	BIC R11, R12, R3; AND R13, R3, R3
	MOVD 80(RSP), R13
	ORR R11, R12, R14; MVN R14, R14; AND R13, R14, R14
	ORR R2, R1, R1
	ORR R3, R1, R1
	ORR R14, R1, R1

	// Writeback uses R17 (active mask, set per-peer in bc_mask_set above
	// — owner mask for TopoHypercube, peer[maskStart] for TopoHypercubePerPeer).
	MOVD R17, R2
	AND $127, R9, R3
	MOVD 104(RSP), R13
	MOVD (R13)(R3<<3), R15
	AND R2, R1, R1
	BIC R2, R15, R15
	ORR R15, R1, R1
	MOVD R1, (R13)(R3<<3)

	ADD $1, R5; ADD $1, R6
	MOVD 8(RSP), R1
	CMP R1, R6; BNE bcast_adv_b
	MOVD 0(RSP), R5; MOVD $0, R6
bcast_adv_b:
	ADD $1, R7; ADD $1, R8
	MOVD 24(RSP), R1
	CMP R1, R8; BNE bcast_adv_done
	MOVD 16(RSP), R7; MOVD $0, R8
bcast_adv_done:
	ADD $1, R9; ADD $1, R4
	B bcast_inner_loop

bcast_inner_done:
bcast_skip_self:
	ADD $1, R25
	B bcast_peer_loop

bcast_done:
	// Converge to the existing emit / next_pc path.
	B inner_done

single_pass:
	// --- 3. ALU INNER LOOP ---
	MOVD $0, R4 // lane = 0
	MOVD 0(RSP), R5; MOVD $0, R6 // idxA, spanA
	MOVD 16(RSP), R7; MOVD $0, R8 // idxB, spanB
	MOVD 32(RSP), R9 // idxDst

inner_loop:
	MOVD 40(RSP), R10
	CMP R10, R4; BHS inner_done

	AND $127, R5, R1
	MOVD 88(RSP), R2
	MOVD (R2)(R1<<3), R11 // wordA

	AND $127, R7, R1
	MOVD 96(RSP), R2
	MOVD (R2)(R1<<3), R12 // wordB

	// bRotate: byte-rotate the SrcB span by N bytes before the op.
	// Reads the next word in the span and combines via shift+OR. Only
	// fires when bRotate != 0; the predicate path is its own block so
	// we don't need to gate on the predicate bit here.
	UBFX $58, R22, $3, R0
	CBZ R0, sp_no_rot
	ADD $1, R8, R3
	MOVD 24(RSP), R16
	CMP R16, R3; BLO sp_have_next
	MOVD $0, R3
sp_have_next:
	MOVD 16(RSP), R16
	ADD R16, R3
	AND $127, R3
	MOVD 96(RSP), R16
	MOVD (R16)(R3<<3), R3
	LSL $3, R0
	LSR R0, R12, R12
	MOVD $64, R16
	SUB R0, R16
	LSL R16, R3
	ORR R3, R12
sp_no_rot:

	// truth table combinations
	MOVD 56(RSP), R13
	AND R12, R11, R1; AND R13, R1, R1 // path0

	MOVD 64(RSP), R13
	BIC R12, R11, R2; AND R13, R2, R2 // path1

	MOVD 72(RSP), R13
	BIC R11, R12, R3; AND R13, R3, R3 // path2

	MOVD 80(RSP), R13
	ORR R11, R12, R14; MVN R14, R14; AND R13, R14, R14 // path3

	// res = OR(paths)
	ORR R2, R1, R1
	ORR R3, R1, R1
	ORR R14, R1, R1

	// predicated writeback
	MOVD 48(RSP), R2 // mask
	AND $127, R9, R3
	MOVD 104(RSP), R13
	MOVD (R13)(R3<<3), R15 // dstWord

	AND R2, R1, R1     // res & mask
	BIC R2, R15, R15   // dstWord & ^mask
	ORR R15, R1, R1
	MOVD R1, (R13)(R3<<3)

	// advance pointers (avoids Modulo DIV latency)
	ADD $1, R5; ADD $1, R6
	MOVD 8(RSP), R1
	CMP R1, R6; BNE adv_b
	MOVD 0(RSP), R5; MOVD $0, R6
adv_b:
	ADD $1, R7; ADD $1, R8
	MOVD 24(RSP), R1
	CMP R1, R8; BNE adv_done
	MOVD 16(RSP), R7; MOVD $0, R8
adv_done:
	ADD $1, R9; ADD $1, R4
	B inner_loop

inner_done:
	// --- 4. EMIT SIGNAL ---
	MOVD 112(RSP), R1
	CMP $1, R1; BNE emit_skip
	MOVD 48(RSP), R1
	CBZ R1, emit_skip

	MOVD 560(R19), R1
	ADD $1, R1
	MOVD R1, 560(R19) // ownerFrame[70] += 1

emit_skip:
	// Convergence: predicate path also lands here so it shares the
	// pop-rewind handling.
popend_check:
	// pop-end rewinding. When popEnd is set on the current instruction
	// AND a pop seed previously activated, advance the lane and rewind
	// pc to the body start so the body sweeps each peer in turn. When
	// the lane is drained, settle popActive and fall through.
	MOVD 152(RSP), R1
	CBZ R1, next_pc
	MOVD 144(RSP), R1
	CBZ R1, next_pc
	MOVD communitySize+48(FP), R2
	CMP R2, R21; BHS pop_lane_drained
	MOVD community_ptr+24(FP), R3
	MOVD (R3)(R21<<3), R23
	MOVD R23, 128(RSP)
	ADD $1, R21
	MOVD 136(RSP), R20
	B pc_loop

pop_lane_drained:
	MOVD ZR, 144(RSP)

next_pc:
	ADD $1, R20
	B pc_loop

// --- PREDICATE PATH ---
// Entered from no_predicate when bit 57 (predicate) is set. Computes
// popcount over the SrcA span and either stores it (PredStorePopcnt),
// converts an any-zero check into a mask (PredAnyZero), or compares
// against ownerFrame[bStart] (LT/LE/GT/GE/EQ/NE) and writes the mask
// to ptrDst[dstStart]. Falls through to popend_check so the same
// pop-rewind machinery handles loop bodies that end on a predicate.
predicate_path:
	// argmin_nonzero is a reduce op encoded as a predicate-flagged
	// instruction with opcode 1. mode_eq stays Go-fallback because it
	// needs a hash overflow that doesn't fit cleanly in scalar asm.
	AND $0xF, R22, R0
	CMP $1, R0; BEQ reduce_argmin_path
	CMP $2, R0; BEQ reduce_mode_eq_path
	CMP $3, R0; BEQ reduce_zipf_path

	// TopoHypercubePerPeer routes to a per-peer evaluation that loops
	// over the community and writes a per-peer mask into peer[dstStart].
	UBFX $55, R22, $2, R0
	CMP $3, R0; BEQ per_peer_predicate_path

	// pop = popcount over [aStart, aStart+aSpan)
	MOVD $0, R3 // pop accumulator
	MOVD $0, R4 // lane counter
pred_pop_loop:
	MOVD 8(RSP), R6
	CMP R6, R4; BHS pred_pop_done
	MOVD 0(RSP), R5
	ADD R4, R5
	AND $127, R5
	MOVD 88(RSP), R1
	MOVD (R1)(R5<<3), R0

	// SWAR popcount: x = x - ((x >> 1) & 0x55..)
	LSR $1, R0, R1
	MOVD $0x5555555555555555, R5
	AND R5, R1
	SUB R1, R0
	// x = (x & 0x33..) + ((x >> 2) & 0x33..)
	LSR $2, R0, R1
	MOVD $0x3333333333333333, R5
	AND R5, R0
	AND R5, R1
	ADD R1, R0
	// x = ((x + (x >> 4)) & 0x0f..)
	LSR $4, R0, R1
	ADD R1, R0
	MOVD $0x0f0f0f0f0f0f0f0f, R5
	AND R5, R0
	// pop = (x * 0x01..) >> 56
	MOVD $0x0101010101010101, R5
	MUL R5, R0
	LSR $56, R0

	ADD R0, R3
	ADD $1, R4
	B pred_pop_loop
pred_pop_done:

	// Dispatch by predCond
	MOVD 168(RSP), R0
	CMP $6, R0; BEQ pred_store_popcnt
	CMP $7, R0; BEQ pred_any_zero
	B pred_compare

pred_store_popcnt:
	// ptrDst[dstStart] = (pop & guard) | (prevDst & ^guard)
	MOVD 32(RSP), R5
	AND $127, R5
	MOVD 104(RSP), R1
	MOVD (R1)(R5<<3), R6
	MOVD 48(RSP), R0
	AND R0, R3
	BIC R0, R6
	ORR R6, R3
	MOVD R3, (R1)(R5<<3)
	B popend_check

pred_any_zero:
	// result = ^0 if any word in A range is zero, else 0
	MOVD $0, R3
	MOVD $0, R4
pa_loop:
	MOVD 8(RSP), R6
	CMP R6, R4; BHS pa_done
	MOVD 0(RSP), R5
	ADD R4, R5
	AND $127, R5
	MOVD 88(RSP), R1
	MOVD (R1)(R5<<3), R0
	CBNZ R0, pa_continue
	MOVD $-1, R3
	B pa_done
pa_continue:
	ADD $1, R4
	B pa_loop
pa_done:
	// ptrDst[dstStart] = (result & guard) | (prevDst & ^guard)
	MOVD 32(RSP), R5
	AND $127, R5
	MOVD 104(RSP), R1
	MOVD (R1)(R5<<3), R6
	MOVD 48(RSP), R0
	AND R0, R3
	BIC R0, R6
	ORR R6, R3
	MOVD R3, (R1)(R5<<3)
	B popend_check

pred_compare:
	// witness = (aSpan == 1) ? ptrA[aStart] : pop
	MOVD 8(RSP), R6
	CMP $1, R6; BNE pc_use_pop
	MOVD 0(RSP), R5
	AND $127, R5
	MOVD 88(RSP), R1
	MOVD (R1)(R5<<3), R3
pc_use_pop:
	// threshold = ownerFrame[bStart]
	MOVD 16(RSP), R5
	AND $127, R5
	MOVD (R19)(R5<<3), R6

	MOVD 168(RSP), R0
	CMP $0, R0; BEQ pc_lt
	CMP $1, R0; BEQ pc_le
	CMP $2, R0; BEQ pc_gt
	CMP $3, R0; BEQ pc_ge
	CMP $4, R0; BEQ pc_eq
	CMP $5, R0; BEQ pc_ne
	B pc_zero
pc_lt:
	CMP R6, R3; BLO pc_one; B pc_zero
pc_le:
	CMP R6, R3; BLS pc_one; B pc_zero
pc_gt:
	CMP R6, R3; BHI pc_one; B pc_zero
pc_ge:
	CMP R6, R3; BHS pc_one; B pc_zero
pc_eq:
	CMP R6, R3; BEQ pc_one; B pc_zero
pc_ne:
	CMP R6, R3; BNE pc_one; B pc_zero
pc_one:
	MOVD $-1, R3
	B pc_writeback
pc_zero:
	MOVD $0, R3
pc_writeback:
	// Default predicate writeback: ptrDst[dstStart] = mask & guard.
	// Unlike StorePopcnt / AnyZero, the compare path overwrites dst
	// rather than preserving prevDst — matches the Go reference.
	MOVD 48(RSP), R0
	AND R0, R3
	MOVD 32(RSP), R5
	AND $127, R5
	MOVD 104(RSP), R1
	MOVD R3, (R1)(R5<<3)
	B popend_check

// --- REDUCE: argmin_nonzero ---
// Mirrors reduceArgMinNonZero in Go. For each peer in the community
// (skipping ownerIdx and nil entries), if peer[valueStart] != 0 and
// peer[keyStart] < bestKey, update bestKey/bestValue. After the scan
// write bestValue to ownerFrame[dstStart] when bestValue != 0. Guard
// at ownerFrame[guardStart] gates the whole pass: zero guard → no-op.
// Note: aStart/bStart from the instruction are the value/key starts,
// dstStart is the dst, maskStart is the guard.
reduce_argmin_path:
	// Guard: ownerFrame[maskStart]
	MOVD 184(RSP), R5
	AND $127, R5
	MOVD (R19)(R5<<3), R6
	CBZ R6, popend_check

	MOVD $0, R3 // bestValue = 0
	MOVD $-1, R4 // bestKey = ^0
	MOVD $0, R25 // k
	MOVD communitySize+48(FP), R26
	MOVD community_ptr+24(FP), R27
ram_loop:
	CMP R26, R25; BHS ram_done
	MOVD (R27)(R25<<3), R7 // peer
	CBZ R7, ram_skip

	// value = peer[valueStart]
	MOVD 0(RSP), R5
	AND $127, R5
	MOVD (R7)(R5<<3), R8
	CBZ R8, ram_skip

	// key = peer[keyStart]
	MOVD 16(RSP), R5
	AND $127, R5
	MOVD (R7)(R5<<3), R9

	CMP R4, R9; BHS ram_skip
	MOVD R9, R4 // bestKey = key
	MOVD R8, R3 // bestValue = value
ram_skip:
	ADD $1, R25
	B ram_loop
ram_done:
	CBZ R3, popend_check
	MOVD 32(RSP), R5
	AND $127, R5
	MOVD R3, (R19)(R5<<3)
	B popend_check

// --- REDUCE: mode_eq ---
// Selects the modal non-zero B[valueStart] where B[keyStart] equals the
// owner match word at maskStart. Ties keep first encounter order.
reduce_mode_eq_path:
	MOVD 184(RSP), R5
	AND $127, R5
	MOVD (R19)(R5<<3), R6 // match
	CBZ R6, popend_check
	MOVD $0, R3  // bestValue
	MOVD $0, R4  // bestCount
	MOVD $0, R25 // idx
	MOVD communitySize+48(FP), R26
	MOVD community_ptr+24(FP), R27
rme_outer_loop:
	CMP R26, R25; BHS rme_done
	MOVD (R27)(R25<<3), R7
	CBZ R7, rme_outer_next
	MOVD 16(RSP), R5
	AND $127, R5
	MOVD (R7)(R5<<3), R8
	CMP R6, R8; BNE rme_outer_next
	MOVD 0(RSP), R5
	AND $127, R5
	MOVD (R7)(R5<<3), R9 // candidate value
	CBZ R9, rme_outer_next
	MOVD $0, R10 // count
	MOVD $0, R11 // otherIdx
rme_inner_loop:
	CMP R26, R11; BHS rme_count_done
	MOVD (R27)(R11<<3), R12
	CBZ R12, rme_inner_next
	MOVD 16(RSP), R5
	AND $127, R5
	MOVD (R12)(R5<<3), R13
	CMP R6, R13; BNE rme_inner_next
	MOVD 0(RSP), R5
	AND $127, R5
	MOVD (R12)(R5<<3), R13
	CMP R9, R13; BNE rme_inner_next
	ADD $1, R10
rme_inner_next:
	ADD $1, R11
	B rme_inner_loop
rme_count_done:
	CMP R4, R10; BLS rme_outer_next
	MOVD R10, R4
	MOVD R9, R3
rme_outer_next:
	ADD $1, R25
	B rme_outer_loop
rme_done:
	CBZ R3, popend_check
	MOVD 32(RSP), R5
	AND $127, R5
	MOVD R3, (R19)(R5<<3)
	B popend_check

// --- REDUCE: zipf_select ---
// Fixed-point Zipfian candidate selection over B[valueStart] ranked by
// B[utilityStart]. Temperature is the owner-side maskStart word; zero is
// greedy. The integer contract mirrors executeKernelGo / Metal / CUDA.
reduce_zipf_path:
	MOVD $0, R3  // count
	MOVD $0, R4  // bestValue
	MOVD $0, R5  // bestUtility
	MOVD $0, R6  // bestIndex
	MOVD $0, R7  // found
	MOVD $0, R25 // idx
	MOVD communitySize+48(FP), R26
	MOVD community_ptr+24(FP), R27
rz_scan_loop:
	CMP R26, R25; BHS rz_scan_done
	MOVD (R27)(R25<<3), R8
	CBZ R8, rz_scan_skip
	MOVD 0(RSP), R9
	AND $127, R9
	MOVD (R8)(R9<<3), R10 // value
	CBZ R10, rz_scan_skip
	ADD $1, R3
	MOVD 16(RSP), R9
	AND $127, R9
	MOVD (R8)(R9<<3), R11 // utility
	CBZ R7, rz_scan_update
	CMP R5, R11; BHI rz_scan_update
	CMP R5, R11; BNE rz_scan_skip
	CMP R6, R25; BLO rz_scan_update
	B rz_scan_skip
rz_scan_update:
	MOVD R10, R4
	MOVD R11, R5
	MOVD R25, R6
	MOVD $1, R7
rz_scan_skip:
	ADD $1, R25
	B rz_scan_loop
rz_scan_done:
	CBZ R3, popend_check
	MOVD 184(RSP), R8
	AND $127, R8
	MOVD (R19)(R8<<3), R8 // temperature
	CBZ R8, rz_write_best
	CMP $1, R3; BEQ rz_write_best

	MOVD $4, R9 // power
	CMP $128, R8; BLO rz_power_done
	MOVD $3, R9
	CMP $256, R8; BLO rz_power_done
	MOVD $2, R9
	CMP $512, R8; BLO rz_power_done
	MOVD $1, R9
	CMP $1024, R8; BLO rz_power_done
	MOVD $0, R9
rz_power_done:
	MOVD $0, R10 // total
	MOVD $1, R11 // rank
rz_total_loop:
	CMP R3, R11; BHI rz_total_done
	CBZ R9, rz_total_weight_uniform
	MOVD $0x1000000000000, R12
	MOVD $0, R16
rz_total_weight_loop:
	CMP R9, R16; BHS rz_total_weight_done
	UDIV R11, R12, R12
	CBNZ R12, rz_total_weight_next
	MOVD $1, R12
	B rz_total_weight_done
rz_total_weight_next:
	ADD $1, R16
	B rz_total_weight_loop
rz_total_weight_uniform:
	MOVD $1, R12
rz_total_weight_done:
	ADD R12, R10
	ADD $1, R11
	B rz_total_loop
rz_total_done:
	CBZ R10, rz_write_best

	// seed = mix(owner.id ^ rotl(epoch,17) ^ rotl(community,31)
	//            ^ rotl(surprisal,7) ^ rotl(bestUtility,43) ^ count)
	MOVD 976(R19), R14
	MOVD 464(R19), R15
	LSL $17, R15, R16
	LSR $47, R15, R15
	ORR R16, R15
	EOR R15, R14
	MOVD 512(R19), R15
	LSL $31, R15, R16
	LSR $33, R15, R15
	ORR R16, R15
	EOR R15, R14
	MOVD 544(R19), R15
	LSL $7, R15, R16
	LSR $57, R15, R15
	ORR R16, R15
	EOR R15, R14
	MOVD R5, R15
	LSL $43, R15, R16
	LSR $21, R15, R15
	ORR R16, R15
	EOR R15, R14
	EOR R3, R14
	MOVD $0x9e3779b97f4a7c15, R15
	ADD R15, R14
	MOVD R14, R15
	LSR $30, R15
	EOR R15, R14
	MOVD $0xbf58476d1ce4e5b9, R15
	MUL R15, R14
	MOVD R14, R15
	LSR $27, R15
	EOR R15, R14
	MOVD $0x94d049bb133111eb, R15
	MUL R15, R14
	MOVD R14, R15
	LSR $31, R15
	EOR R15, R14

	UDIV R10, R14, R15
	MSUB R15, R10, R14, R17 // ticket

	MOVD $0, R13 // running
	MOVD $1, R11 // rank
rz_pick_loop:
	CMP R3, R11; BHI rz_pick_fallback_last
	CBZ R9, rz_pick_weight_uniform
	MOVD $0x1000000000000, R12
	MOVD $0, R16
rz_pick_weight_loop:
	CMP R9, R16; BHS rz_pick_weight_done
	UDIV R11, R12, R12
	CBNZ R12, rz_pick_weight_next
	MOVD $1, R12
	B rz_pick_weight_done
rz_pick_weight_next:
	ADD $1, R16
	B rz_pick_weight_loop
rz_pick_weight_uniform:
	MOVD $1, R12
rz_pick_weight_done:
	ADD R12, R13
	CMP R13, R17; BLO rz_have_target
	ADD $1, R11
	B rz_pick_loop
rz_pick_fallback_last:
	MOVD R3, R11
rz_have_target:
	MOVD R11, 168(RSP) // targetRank; predCond no longer needed
	B rz_candidate_select

rz_write_best:
	MOVD 32(RSP), R8
	AND $127, R8
	MOVD R4, (R19)(R8<<3)
	B popend_check

rz_candidate_select:
	MOVD $0, R25 // idx
rz_candidate_outer:
	CMP R26, R25; BHS rz_write_best
	MOVD (R27)(R25<<3), R8
	CBZ R8, rz_candidate_next
	MOVD 0(RSP), R9
	AND $127, R9
	MOVD (R8)(R9<<3), R10 // value
	CBZ R10, rz_candidate_next
	MOVD 16(RSP), R9
	AND $127, R9
	MOVD (R8)(R9<<3), R11 // utility
	MOVD $1, R12 // rank
	MOVD $0, R13 // otherIdx
rz_candidate_inner:
	CMP R26, R13; BHS rz_candidate_rank_done
	CMP R25, R13; BEQ rz_candidate_inner_next
	MOVD (R27)(R13<<3), R14
	CBZ R14, rz_candidate_inner_next
	MOVD 0(RSP), R15
	AND $127, R15
	MOVD (R14)(R15<<3), R15
	CBZ R15, rz_candidate_inner_next
	MOVD 16(RSP), R15
	AND $127, R15
	MOVD (R14)(R15<<3), R15 // otherUtility
	CMP R11, R15; BHI rz_candidate_rank_inc
	CMP R11, R15; BNE rz_candidate_inner_next
	CMP R25, R13; BLO rz_candidate_rank_inc
	B rz_candidate_inner_next
rz_candidate_rank_inc:
	ADD $1, R12
rz_candidate_inner_next:
	ADD $1, R13
	B rz_candidate_inner
rz_candidate_rank_done:
	MOVD 168(RSP), R8
	CMP R8, R12; BNE rz_candidate_next
	MOVD 32(RSP), R8
	AND $127, R8
	MOVD R10, (R19)(R8<<3)
	B popend_check
rz_candidate_next:
	ADD $1, R25
	B rz_candidate_outer

// --- PER-PEER PREDICATE PATH (TopoHypercubePerPeer) ---
// Loops over the community. For each peer evaluates the comparison
// using either owner or peer as the witness source (controlled by
// srcAFromB) and writes ^0 / 0 to peer[dstStart] — the per-peer mask
// scratch slot that subsequent body instructions in the same gossip
// block read via maskStart. Falls through to popend_check.
per_peer_predicate_path:
	MOVD $0, R25 // peer_k
ppp_loop:
	MOVD communitySize+48(FP), R26
	CMP R26, R25; BHS popend_check
	MOVD ownerIdx+16(FP), R26
	CMP R26, R25; BEQ ppp_skip
	MOVD community_ptr+24(FP), R26
	MOVD (R26)(R25<<3), R27 // peer
	CBZ R27, ppp_skip

	// witnessSrc = (srcAFromB == 1) ? peer : owner
	UBFX $61, R22, $1, R0
	MOVD R19, R26
	CMP $1, R0; BNE ppp_have_src
	MOVD R27, R26
ppp_have_src:

	// pop = popcount over [aStart, aSpan) of witnessSrc
	MOVD $0, R3
	MOVD $0, R4
ppp_pop_loop:
	MOVD 8(RSP), R6
	CMP R6, R4; BHS ppp_pop_done
	MOVD 0(RSP), R5
	ADD R4, R5
	AND $127, R5
	MOVD (R26)(R5<<3), R0

	LSR $1, R0, R1
	MOVD $0x5555555555555555, R5
	AND R5, R1
	SUB R1, R0
	LSR $2, R0, R1
	MOVD $0x3333333333333333, R5
	AND R5, R0
	AND R5, R1
	ADD R1, R0
	LSR $4, R0, R1
	ADD R1, R0
	MOVD $0x0f0f0f0f0f0f0f0f, R5
	AND R5, R0
	MOVD $0x0101010101010101, R5
	MUL R5, R0
	LSR $56, R0

	ADD R0, R3
	ADD $1, R4
	B ppp_pop_loop
ppp_pop_done:

	// witness = (aSpan == 1) ? witnessSrc[aStart] : pop
	MOVD 8(RSP), R6
	CMP $1, R6; BNE ppp_witness_done
	MOVD 0(RSP), R5
	AND $127, R5
	MOVD (R26)(R5<<3), R3
ppp_witness_done:

	// threshold = ownerFrame[bStart]
	MOVD 16(RSP), R5
	AND $127, R5
	MOVD (R19)(R5<<3), R6

	MOVD 168(RSP), R0
	CMP $0, R0; BEQ pp_lt
	CMP $1, R0; BEQ pp_le
	CMP $2, R0; BEQ pp_gt
	CMP $3, R0; BEQ pp_ge
	CMP $4, R0; BEQ pp_eq
	CMP $5, R0; BEQ pp_ne
	B pp_zero
pp_lt:
	CMP R6, R3; BLO pp_one; B pp_zero
pp_le:
	CMP R6, R3; BLS pp_one; B pp_zero
pp_gt:
	CMP R6, R3; BHI pp_one; B pp_zero
pp_ge:
	CMP R6, R3; BHS pp_one; B pp_zero
pp_eq:
	CMP R6, R3; BEQ pp_one; B pp_zero
pp_ne:
	CMP R6, R3; BNE pp_one; B pp_zero
pp_one:
	MOVD $-1, R3
	B ppp_writeback
pp_zero:
	MOVD $0, R3
ppp_writeback:
	MOVD 32(RSP), R5
	AND $127, R5
	MOVD R3, (R27)(R5<<3)

ppp_skip:
	ADD $1, R25
	B ppp_loop

// --- STAGE(B) PATH ---
// Records peer indices into the host-supplied stageBuf so the
// orchestrator can translate them into kernel.StageRequest entries.
// Single-peer mode stages the currentB the most recent pop seed
// bound; TopoHypercubePerPeer iterates the community and stages every
// peer whose per-peer mask word at maskStart is non-zero.
stage_path:
	UBFX $55, R22, $2, R0
	CMP $3, R0; BEQ stage_per_peer

	// Single-peer stage: currentB is at 128(RSP); the peer index we
	// most recently popped is R21-1 (bQueueIdx already advanced).
	MOVD 128(RSP), R0
	CBZ R0, popend_check
	SUB $1, R21, R0
	MOVD stageCount+72(FP), R1
	MOVD (R1), R2
	CMP $128, R2; BHS popend_check
	MOVD stageBuf+64(FP), R3
	MOVD R0, (R3)(R2<<3)
	ADD $1, R2
	MOVD R2, (R1)
	B popend_check

stage_per_peer:
	MOVD $0, R0 // k = 0
sp_stage_loop:
	MOVD communitySize+48(FP), R1
	CMP R1, R0; BHS popend_check
	MOVD ownerIdx+16(FP), R1
	CMP R1, R0; BEQ sp_stage_skip
	MOVD community_ptr+24(FP), R1
	MOVD (R1)(R0<<3), R3
	CBZ R3, sp_stage_skip
	MOVD 184(RSP), R5
	AND $127, R5
	MOVD (R3)(R5<<3), R6
	CBZ R6, sp_stage_skip

	// Append k to stageBuf
	MOVD stageCount+72(FP), R1
	MOVD (R1), R2
	CMP $128, R2; BHS popend_check
	MOVD stageBuf+64(FP), R3
	MOVD R0, (R3)(R2<<3)
	ADD $1, R2
	MOVD R2, (R1)
sp_stage_skip:
	ADD $1, R0
	B sp_stage_loop

end_pc_loop:
	RET
