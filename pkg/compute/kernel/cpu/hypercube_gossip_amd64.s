//go:build amd64
#include "textflag.h"

// func (backend *Backend) executeKernel(...)
// Total argument footprint: 64 bytes (8 receiver + 56 args)
TEXT ·executeKernel(SB), NOSPLIT, $192-80
	MOVQ ownerFrame+8(FP), DI
	XORQ R11, R11 // pc = 0
	XORQ R12, R12
	// Reserved high-bit scratch kept for ABI-sized stack frames.
	XORQ AX, AX
	MOVQ AX, 128(SP)
	MOVQ AX, 136(SP)
	MOVQ AX, 144(SP)
	MOVQ AX, 152(SP)

pc_loop:
	CMPQ R11, $16
	JGE end_pc_loop

	// instr = ownerFrame[16+pc]
	MOVQ R11, AX
	ADDQ $16, AX
	MOVQ (DI)(AX*8), R14

	TESTQ R14, R14
	JZ next_pc

	// --- 1. DECODE INSTRUCTION ---
	MOVQ R14, AX
	ANDQ $0xF, AX
	
	// m0 = -(opcode & 1)
	MOVQ AX, CX
	ANDQ $1, CX
	NEGQ CX
	MOVQ CX, 56(SP)

	// m1 = -((opcode >> 1) & 1)
	MOVQ AX, CX
	SHRQ $1, CX
	ANDQ $1, CX
	NEGQ CX
	MOVQ CX, 64(SP)

	// m2 = -((opcode >> 2) & 1)
	MOVQ AX, CX
	SHRQ $2, CX
	ANDQ $1, CX
	NEGQ CX
	MOVQ CX, 72(SP)

	// m3 = -((opcode >> 3) & 1)
	MOVQ AX, CX
	SHRQ $3, CX
	ANDQ $1, CX
	NEGQ CX
	MOVQ CX, 80(SP)

	// Spans and Offsets
	MOVQ R14, AX; SHRQ $4, AX; ANDQ $0x7F, AX; MOVQ AX, 0(SP) // aStart
	MOVQ R14, AX; SHRQ $11, AX; ANDQ $0x7F, AX; INCQ AX; MOVQ AX, 8(SP) // aSpan
	MOVQ R14, AX; SHRQ $18, AX; ANDQ $0x7F, AX; MOVQ AX, 16(SP) // bStart
	MOVQ R14, AX; SHRQ $25, AX; ANDQ $0x7F, AX; INCQ AX; MOVQ AX, 24(SP) // bSpan
	MOVQ R14, AX; SHRQ $32, AX; ANDQ $0x7F, AX; MOVQ AX, 32(SP) // dstStart
	MOVQ R14, AX; SHRQ $39, AX; ANDQ $0x7F, AX; INCQ AX; MOVQ AX, 40(SP) // dstSpan

	// mask = ownerFrame[maskStart], also save maskStart for per-peer
	// reads (e.g. TopoHypercubePerPeer stage check).
	MOVQ R14, AX; SHRQ $46, AX; ANDQ $0x7F, AX; MOVQ AX, 184(SP); MOVQ (DI)(AX*8), CX; MOVQ CX, 48(SP)

	// emit flag
	MOVQ R14, AX; SHRQ $54, AX; ANDQ $1, AX; MOVQ AX, 112(SP)

	// bRotate (bits 58-60 when predicate=0). Saved to stack because R14
	// is reused inside the inner loop for wordA, so we cannot re-decode
	// it cheaply later. arm64 keeps instr in R22 across the inner loop
	// and does not need this stash.
	MOVQ R14, AX; SHRQ $58, AX; ANDQ $7, AX; MOVQ AX, 120(SP)

	// popEnd is reserved in strict firmware; decoded only for dump parity.
	MOVQ R14, AX; SHRQ $63, AX; ANDQ $1, AX; MOVQ AX, 152(SP)

	// predicate (bit 57) and predCond (bits 58-60) for the predicate
	// dispatch.
	MOVQ R14, AX; SHRQ $57, AX; ANDQ $1, AX; MOVQ AX, 160(SP)
	MOVQ R14, AX; SHRQ $58, AX; ANDQ $7, AX; MOVQ AX, 168(SP)

	// stageBit is reserved in strict firmware.
	MOVQ R14, AX; SHRQ $62, AX; ANDQ $1, AX; MOVQ AX, 176(SP)

	// targetB flag
	MOVQ R14, AX; SHRQ $53, AX; ANDQ $1, AX; MOVQ AX, R15

	// topology 
	MOVQ R14, AX; SHRQ $55, AX; ANDQ $3, AX

	// --- 2. TOPOLOGY ROUTING ---
	// Default ptrB to nil. Gossip routing may override; otherwise the
	// failsafe in topo_done routes the read back at the owner frame.
	MOVQ 128(SP), R13
	MOVQ communitySize+48(FP), R9
	MOVQ community_ptr+24(FP), SI

	CMPQ AX, $2; JEQ topo_hyper
	JMP topo_done

topo_hyper:
	MOVQ dimCount+56(FP), R10
	TESTQ R10, R10; JZ topo_done

	// pc % dimCount
	MOVQ R11, AX
	XORQ DX, DX
	DIVQ R10            // AX = pc/dimCount, DX = pc%dimCount

	// CX = 1 << DX. Variable shifts on amd64 require the count in CL,
	// so move DX into CX first and shift a fresh 1 by CL into CX.
	MOVQ DX, CX         // CL = pc%dimCount
	MOVQ $1, AX
	SHLQ CX, AX         // AX = 1 << CL
	MOVQ AX, CX         // CX = 1 << dim
	MOVQ ownerIdx+16(FP), BX
	XORQ CX, BX         // BX = peerIdx

	CMPQ BX, R9; JAE topo_fallback
	MOVQ (SI)(BX*8), R13
	JMP topo_done
topo_fallback:
	XORQ R13, R13 // nil

topo_done:
	TESTQ R13, R13; JNZ ptrb_ok
	MOVQ DI, R13 // failsafe
ptrb_ok:
	MOVQ R13, 96(SP) // ptrB

	// srcAFromB routing: bit 61 makes ptrA point at the bound peer
	// instead of the owner, so `write B.x <- xor(B.x, B.y)` works
	// without reverse-engineering the operand routing.
	MOVQ R14, AX; SHRQ $61, AX; ANDQ $1, AX
	TESTQ AX, AX; JZ ptra_owner
	MOVQ R13, 88(SP)
	JMP ptra_done
ptra_owner:
	MOVQ DI, 88(SP)
ptra_done:

	MOVQ DI, CX
	CMPQ R15, $1; JNE ptrdst_ok
	MOVQ R13, CX
ptrdst_ok:
	MOVQ CX, 104(SP) // ptrDst

	// Predicate dispatch (mirrors arm64 no_predicate gate). When the
	// predicate bit is set the kernel computes a popcount-based mask
	// and skips the truth-table broadcast.
	CMPQ 160(SP), $1; JNE no_predicate_amd64
	JMP predicate_path_amd64
no_predicate_amd64:

	// --- 2b. PER-PEER HYPERCUBE BROADCAST ROUTING ---
	// When topology == 2 (Hypercube) AND target == B, the new ALU
	// writes the truth-table result to EVERY peer's dst on the same
	// instruction (broadcast). Mirrors the arm64 bcast_peer_loop.
	// Single-pass code below handles all other cases; emit fires once
	// after every peer is written, matching the Go reference.
	MOVQ R14, AX; SHRQ $55, AX; ANDQ $3, AX
	CMPQ AX, $2; JE bcast_target_check_amd64
	CMPQ AX, $3; JE bcast_target_check_amd64
	JMP single_pass_amd64
bcast_target_check_amd64:
	MOVQ R14, AX; SHRQ $53, AX; ANDQ $1, AX
	CMPQ AX, $1; JNE single_pass_amd64

	XORQ R15, R15 // peer_k = 0
bcast_peer_loop:
	MOVQ communitySize+48(FP), AX
	CMPQ R15, AX; JAE bcast_done
	MOVQ ownerIdx+16(FP), AX
	CMPQ R15, AX; JE bcast_skip_self
	MOVQ community_ptr+24(FP), AX
	MOVQ (AX)(R15*8), R13
	TESTQ R13, R13; JZ bcast_skip_self
	MOVQ R13, 96(SP)
	MOVQ R13, 104(SP)

	// Per-peer mask: TopoHypercubePerPeer reads peer[maskStart] each
	// iteration; TopoHypercube keeps the owner-side mask. Saved to
	// 192(SP) so the inner loop can read it without juggling regs.
	MOVQ R14, AX; SHRQ $55, AX; ANDQ $3, AX
	CMPQ AX, $3; JNE bc_owner_mask_amd64
	MOVQ 184(SP), AX
	ANDQ $127, AX
	MOVQ (R13)(AX*8), AX
	JMP bc_mask_set_amd64
bc_owner_mask_amd64:
	MOVQ 48(SP), AX
bc_mask_set_amd64:
	// Keep the active mask in BX through the inner loop; the truth-table
	// path only needs it after wordA/wordB are loaded.
	// Actually that's fragile. Save active mask to 48(SP) and re-load
	// owner mask before emit_check via a stash slot...
	// Cleanest: save owner mask once before bcast, restore after.
	MOVQ AX, 48(SP)

	// Inlined truth-table inner loop for this peer.
	XORQ BX, BX     // lane = 0
	MOVQ 0(SP), R8  // idxA
	XORQ R9, R9     // spanA
	MOVQ 16(SP), R10
	XORQ R12, R12   // spanB scratch (clobbers bQueueIdx; restored below)
	MOVQ 32(SP), BP

bcast_inner_loop:
	CMPQ BX, 40(SP)
	JAE bcast_skip_self

	MOVQ 88(SP), SI
	MOVQ R8, AX
	ANDQ $127, AX
	MOVQ (SI)(AX*8), R14

	MOVQ 96(SP), SI
	MOVQ R10, AX
	ANDQ $127, AX
	MOVQ (SI)(AX*8), CX

	// bRotate (broadcast variant). Note: R13 is reused as a scratch
	// register inside this block (path1 etc.), so we juggle through
	// R12/AX/CX/DX during the rotation. R12 (spanB scratch) is loaded
	// fresh after the rotation completes, so clobbering it temporarily
	// is safe.
	MOVQ 120(SP), R13
	TESTQ R13, R13; JZ bc_no_rot_amd64
	MOVQ R12, AX; INCQ AX
	CMPQ AX, 24(SP); JB bc_have_next_amd64
	XORQ AX, AX
bc_have_next_amd64:
	ADDQ 16(SP), AX
	ANDQ $127, AX
	MOVQ 96(SP), DX
	MOVQ (DX)(AX*8), AX
	SHLQ $3, R13
	MOVQ CX, DX
	MOVQ R13, CX
	SHRQ CX, DX
	MOVQ $64, R13
	SUBQ CX, R13
	MOVQ R13, CX
	SHLQ CX, AX
	ORQ AX, DX
	MOVQ DX, CX
bc_no_rot_amd64:

	MOVQ R14, AX
	ANDQ CX, AX
	ANDQ 56(SP), AX

	MOVQ CX, DX
	NOTQ DX
	MOVQ R14, R13
	ANDQ DX, R13
	ANDQ 64(SP), R13
	ORQ R13, AX

	MOVQ R14, DX
	NOTQ DX
	MOVQ CX, R13
	ANDQ DX, R13
	ANDQ 72(SP), R13
	ORQ R13, AX

	MOVQ R14, DX
	NOTQ DX
	MOVQ CX, R13
	NOTQ R13
	ANDQ DX, R13
	ANDQ 80(SP), R13
	ORQ R13, AX

	MOVQ 104(SP), SI
	MOVQ BP, DX
	ANDQ $127, DX

	MOVQ 48(SP), R13
	MOVQ R13, CX
	NOTQ CX

	ANDQ R13, AX
	MOVQ (SI)(DX*8), R13
	ANDQ CX, R13
	ORQ R13, AX
	MOVQ AX, (SI)(DX*8)

	INCQ R8; INCQ R9
	CMPQ R9, 8(SP); JNE bcast_adv_b
	MOVQ 0(SP), R8; XORQ R9, R9
bcast_adv_b:
	INCQ R10; INCQ R12
	CMPQ R12, 24(SP); JNE bcast_adv_done
	MOVQ 16(SP), R10; XORQ R12, R12
bcast_adv_done:
	INCQ BP; INCQ BX
	JMP bcast_inner_loop

bcast_skip_self:
	INCQ R15
	JMP bcast_peer_loop

bcast_done:
	// Reset R12 (bQueueIdx) since the broadcast block reused it as a
	// scratch span counter. The next topology=Pop seed reinitializes
	// it explicitly, so zero is safe.
	XORQ R12, R12
	// Restore original owner mask to 48(SP) for the emit check below.
	// The bcast loop overwrote it with the active per-peer mask.
	MOVQ ownerFrame+8(FP), DI
	MOVQ R14, AX; SHRQ $46, AX; ANDQ $0x7F, AX
	MOVQ (DI)(AX*8), AX
	MOVQ AX, 48(SP)
	JMP inner_done

single_pass_amd64:
	// --- 3. ALU INNER LOOP ---
	XORQ R15, R15   // lane = 0
	MOVQ 0(SP), R8  // idxA = aStart
	XORQ R9, R9     // spanA = 0
	MOVQ 16(SP), R10 // idxB = bStart
	XORQ R13, R13   // spanB = 0
	MOVQ 32(SP), BP // idxDst = dstStart

inner_loop:
	CMPQ R15, 40(SP)
	JAE inner_done

	// load wordA
	MOVQ 88(SP), SI
	MOVQ R8, AX
	ANDQ $127, AX
	MOVQ (SI)(AX*8), R14

	// load wordB
	MOVQ 96(SP), SI
	MOVQ R10, AX
	ANDQ $127, AX
	MOVQ (SI)(AX*8), CX

	// bRotate (single-pass). Mirrors sp_no_rot in arm64: read the next
	// word in the SrcB span and combine via shift+OR. amd64 variable
	// shifts require CL, so we juggle through CX/DX/BX.
	MOVQ 120(SP), BX
	TESTQ BX, BX; JZ sp_no_rot_amd64
	MOVQ R13, AX; INCQ AX
	CMPQ AX, 24(SP); JB sp_have_next_amd64
	XORQ AX, AX
sp_have_next_amd64:
	ADDQ 16(SP), AX
	ANDQ $127, AX
	MOVQ 96(SP), DX
	MOVQ (DX)(AX*8), AX
	SHLQ $3, BX
	MOVQ CX, DX
	MOVQ BX, CX
	SHRQ CX, DX
	MOVQ $64, BX
	SUBQ CX, BX
	MOVQ BX, CX
	SHLQ CX, AX
	ORQ AX, DX
	MOVQ DX, CX
sp_no_rot_amd64:

	// truth table path0: (wordA & wordB & m0)
	MOVQ R14, AX
	ANDQ CX, AX
	ANDQ 56(SP), AX

	// truth table path1: (wordA & ^wordB & m1)
	MOVQ CX, DX
	NOTQ DX
	MOVQ R14, BX
	ANDQ DX, BX
	ANDQ 64(SP), BX
	ORQ BX, AX

	// truth table path2: (^wordA & wordB & m2)
	MOVQ R14, DX
	NOTQ DX
	MOVQ CX, BX
	ANDQ DX, BX
	ANDQ 72(SP), BX
	ORQ BX, AX

	// truth table path3: (^wordA & ^wordB & m3)
	MOVQ R14, DX
	NOTQ DX
	MOVQ CX, BX
	NOTQ BX
	ANDQ DX, BX
	ANDQ 80(SP), BX
	ORQ BX, AX

	// predicated writeback
	MOVQ 104(SP), SI
	MOVQ BP, DX
	ANDQ $127, DX

	MOVQ 48(SP), BX // mask
	MOVQ BX, CX
	NOTQ CX // ^mask

	ANDQ BX, AX
	MOVQ (SI)(DX*8), BX
	ANDQ CX, BX
	ORQ BX, AX
	MOVQ AX, (SI)(DX*8)

	// cycle bounds (avoids DIV modulo)
	INCQ R8; INCQ R9
	CMPQ R9, 8(SP); JNE adv_b
	MOVQ 0(SP), R8; XORQ R9, R9
adv_b:
	INCQ R10; INCQ R13
	CMPQ R13, 24(SP); JNE adv_done
	MOVQ 16(SP), R10; XORQ R13, R13
adv_done:
	INCQ BP; INCQ R15
	JMP inner_loop

inner_done:
	// --- 4. EMIT SIGNAL ---
	CMPQ 112(SP), $1; JNE emit_skip_amd64
	MOVQ 48(SP), AX
	TESTQ AX, AX; JZ emit_skip_amd64
	MOVQ ownerFrame+8(FP), DI
	INCQ 560(DI) // ownerFrame[70] += 1

emit_skip_amd64:
popend_check_amd64:
	JMP next_pc

next_pc:
	MOVQ ownerFrame+8(FP), DI // restore ownerFrame
	INCQ R11
	JMP pc_loop

// --- PREDICATE PATH (amd64) ---
// Mirrors the arm64 predicate_path block. POPCNTQ is the amd64
// popcount; otherwise the dispatch on predCond is identical.
predicate_path_amd64:
	// TopoHypercubePerPeer routes to per-peer evaluation that loops
	// over the community and writes a per-peer mask into peer[dstStart].
	MOVQ R14, AX; SHRQ $55, AX; ANDQ $3, AX
	CMPQ AX, $3; JE per_peer_predicate_path_amd64

	XORQ R14, R14 // pop accumulator (R14 was instr; instr no longer needed)
	XORQ R15, R15 // lane counter
pred_pop_loop_amd64:
	CMPQ R15, 8(SP)
	JAE pred_pop_done_amd64
	MOVQ 0(SP), AX
	ADDQ R15, AX
	ANDQ $127, AX
	MOVQ 88(SP), SI
	MOVQ (SI)(AX*8), CX
	POPCNTQ CX, BX
	ADDQ BX, R14
	INCQ R15
	JMP pred_pop_loop_amd64
pred_pop_done_amd64:

	MOVQ 168(SP), AX
	CMPQ AX, $6; JE pred_store_popcnt_amd64
	JMP pred_compare_amd64

pred_store_popcnt_amd64:
	MOVQ 32(SP), AX
	ANDQ $127, AX
	MOVQ 104(SP), SI
	MOVQ (SI)(AX*8), DX
	MOVQ 48(SP), CX
	MOVQ CX, BX
	NOTQ BX
	ANDQ CX, R14
	ANDQ BX, DX
	ORQ DX, R14
	MOVQ R14, (SI)(AX*8)
	JMP popend_check_amd64

pred_compare_amd64:
	// witness = (aSpan == 1) ? ptrA[aStart] : pop
	CMPQ 8(SP), $1
	JNE pc_use_pop_amd64
	MOVQ 0(SP), AX
	ANDQ $127, AX
	MOVQ 88(SP), SI
	MOVQ (SI)(AX*8), R14
pc_use_pop_amd64:
	// threshold = ownerFrame[bStart]
	MOVQ 16(SP), AX
	ANDQ $127, AX
	MOVQ ownerFrame+8(FP), DI
	MOVQ (DI)(AX*8), DX

	MOVQ 168(SP), AX
	CMPQ AX, $0; JE pc_lt_amd64
	CMPQ AX, $1; JE pc_le_amd64
	CMPQ AX, $2; JE pc_gt_amd64
	CMPQ AX, $3; JE pc_ge_amd64
	CMPQ AX, $4; JE pc_eq_amd64
	CMPQ AX, $5; JE pc_ne_amd64
	JMP pc_zero_amd64
pc_lt_amd64:
	CMPQ R14, DX; JB pc_one_amd64; JMP pc_zero_amd64
pc_le_amd64:
	CMPQ R14, DX; JBE pc_one_amd64; JMP pc_zero_amd64
pc_gt_amd64:
	CMPQ R14, DX; JA pc_one_amd64; JMP pc_zero_amd64
pc_ge_amd64:
	CMPQ R14, DX; JAE pc_one_amd64; JMP pc_zero_amd64
pc_eq_amd64:
	CMPQ R14, DX; JE pc_one_amd64; JMP pc_zero_amd64
pc_ne_amd64:
	CMPQ R14, DX; JNE pc_one_amd64; JMP pc_zero_amd64
pc_one_amd64:
	MOVQ $-1, R14
	JMP pc_writeback_amd64
pc_zero_amd64:
	XORQ R14, R14
pc_writeback_amd64:
	MOVQ 48(SP), CX
	ANDQ CX, R14
	MOVQ 32(SP), AX
	ANDQ $127, AX
	MOVQ 104(SP), SI
	MOVQ R14, (SI)(AX*8)
	JMP popend_check_amd64

// --- PER-PEER PREDICATE PATH (amd64, TopoHypercubePerPeer) ---
// Mirror of arm64 per_peer_predicate_path. Loops over the community,
// evaluates the comparison per peer using either owner or peer as the
// witness source (controlled by srcAFromB), and writes ^0 / 0 to
// peer[dstStart].
per_peer_predicate_path_amd64:
	XORQ R15, R15 // peer_k
ppp_loop_amd64:
	MOVQ communitySize+48(FP), AX
	CMPQ R15, AX; JAE popend_check_amd64
	MOVQ ownerIdx+16(FP), AX
	CMPQ R15, AX; JE ppp_skip_amd64
	MOVQ community_ptr+24(FP), AX
	MOVQ (AX)(R15*8), R13 // peer
	TESTQ R13, R13; JZ ppp_skip_amd64

	// witnessSrc: owner by default, peer when srcAFromB==1.
	MOVQ ownerFrame+8(FP), DI
	MOVQ R14, AX; SHRQ $61, AX; ANDQ $1, AX
	TESTQ AX, AX; JZ ppp_have_src_amd64
	MOVQ R13, DI
ppp_have_src_amd64:

	// pop = popcount over [aStart, aSpan) of witnessSrc
	XORQ R14, R14 // pop accumulator (re-borrow R14)
	XORQ BX, BX   // lane
ppp_pop_loop_amd64:
	CMPQ BX, 8(SP)
	JAE ppp_pop_done_amd64
	MOVQ 0(SP), AX
	ADDQ BX, AX
	ANDQ $127, AX
	MOVQ (DI)(AX*8), CX
	POPCNTQ CX, DX
	ADDQ DX, R14
	INCQ BX
	JMP ppp_pop_loop_amd64
ppp_pop_done_amd64:

	// witness = (aSpan == 1) ? witnessSrc[aStart] : pop
	CMPQ 8(SP), $1
	JNE ppp_witness_done_amd64
	MOVQ 0(SP), AX
	ANDQ $127, AX
	MOVQ (DI)(AX*8), R14
ppp_witness_done_amd64:

	// threshold = ownerFrame[bStart]
	MOVQ ownerFrame+8(FP), DI
	MOVQ 16(SP), AX
	ANDQ $127, AX
	MOVQ (DI)(AX*8), DX

	MOVQ 168(SP), AX
	CMPQ AX, $0; JE pp_lt_amd64
	CMPQ AX, $1; JE pp_le_amd64
	CMPQ AX, $2; JE pp_gt_amd64
	CMPQ AX, $3; JE pp_ge_amd64
	CMPQ AX, $4; JE pp_eq_amd64
	CMPQ AX, $5; JE pp_ne_amd64
	JMP pp_zero_amd64
pp_lt_amd64:
	CMPQ R14, DX; JB pp_one_amd64; JMP pp_zero_amd64
pp_le_amd64:
	CMPQ R14, DX; JBE pp_one_amd64; JMP pp_zero_amd64
pp_gt_amd64:
	CMPQ R14, DX; JA pp_one_amd64; JMP pp_zero_amd64
pp_ge_amd64:
	CMPQ R14, DX; JAE pp_one_amd64; JMP pp_zero_amd64
pp_eq_amd64:
	CMPQ R14, DX; JE pp_one_amd64; JMP pp_zero_amd64
pp_ne_amd64:
	CMPQ R14, DX; JNE pp_one_amd64; JMP pp_zero_amd64
pp_one_amd64:
	MOVQ $-1, R14
	JMP ppp_writeback_amd64
pp_zero_amd64:
	XORQ R14, R14
ppp_writeback_amd64:
	MOVQ 32(SP), AX
	ANDQ $127, AX
	MOVQ R14, (R13)(AX*8)

ppp_skip_amd64:
	INCQ R15
	JMP ppp_loop_amd64

end_pc_loop:
	RET
