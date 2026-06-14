//go:build arm64
#include "textflag.h"

TEXT ·executeKernel(SB), NOSPLIT, $192-80
	MOVD ownerFrame+8(FP), R19
	MOVD $0, R20 // pc
	MOVD $0, R21
	// Reserved high-bit scratch kept for ABI-sized stack frames.
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
	UBFX $63, R22, $1, R0; MOVD R0, 152(RSP) // reserved popEnd
	UBFX $57, R22, $1, R0; MOVD R0, 160(RSP) // predicate
	UBFX $58, R22, $3, R0; MOVD R0, 168(RSP) // predCond
	UBFX $62, R22, $1, R0; MOVD R0, 176(RSP) // reserved stageBit
	UBFX $55, R22, $2, R0 // topology

	// --- 2. TOPOLOGY ROUTING ---
	// Default ptrB to nil. Gossip routing may override; otherwise the
	// failsafe in topo_done routes the read back at the owner frame.
	MOVD 128(RSP), R23

	CMP $2, R0; BEQ topo_hyper
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
	// Convergence: predicate path also lands here and advances to the
	// next linear slot.
popend_check:
	B next_pc

next_pc:
	ADD $1, R20
	B pc_loop

// --- PREDICATE PATH ---
// Entered from no_predicate when bit 57 (predicate) is set. Computes
// popcount over the SrcA span and either stores it (PredStorePopcnt), or
// compares against ownerFrame[bStart] (LT/LE/GT/GE/EQ/NE) and writes the mask
// to ptrDst[dstStart]. Falls through to popend_check so the sweep advances
// to the next slot exactly once.
predicate_path:
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

end_pc_loop:
	RET
