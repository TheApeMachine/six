//go:build arm64
#include "textflag.h"

TEXT ·executeKernel(SB), NOSPLIT, $128-64
	MOVD ownerFrame+8(FP), R19
	MOVD $0, R20 // pc
	MOVD $0, R21 // bQueueIdx

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
	
	// mask = ownerFrame[maskStart]
	UBFX $46, R22, $7, R0
	LSL $3, R0
	ADD R19, R0, R1
	MOVD (R1), R1
	MOVD R1, 48(RSP)

	UBFX $54, R22, $1, R0; MOVD R0, 112(RSP) // emit
	UBFX $55, R22, $2, R0 // topology

	// --- 2. TOPOLOGY ROUTING ---
	MOVD R19, R23 // ptrB defaults to ownerFrame
	
	CMP $1, R0; BEQ topo_pop
	CMP $2, R0; BEQ topo_hyper
	B topo_done

topo_pop:
	MOVD communitySize+48(FP), R1
	CMP R1, R21; BHS topo_done // if bQueueIdx >= communitySize
	MOVD community_ptr+24(FP), R2
	MOVD (R2)(R21<<3), R23
	ADD $1, R21
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
	MOVD R19, 88(RSP)

	// Target B pointer routing
	UBFX $53, R22, $1, R0
	MOVD R19, R24 // ptrDst = ownerFrame
	CMP $1, R0; BNE ptrdst_ok
	MOVD R23, R24 // ptrDst = ptrB
ptrdst_ok:
	MOVD R24, 104(RSP)

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
	CMP $1, R1; BNE next_pc
	MOVD 48(RSP), R1
	CBZ R1, next_pc
	
	MOVD 560(R19), R1
	ADD $1, R1
	MOVD R1, 560(R19) // ownerFrame[70] += 1

next_pc:
	ADD $1, R20
	B pc_loop

end_pc_loop:
	RET
