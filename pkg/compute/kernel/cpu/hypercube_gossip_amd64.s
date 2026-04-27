//go:build amd64
#include "textflag.h"

// func (backend *Backend) executeKernel(...)
// Total argument footprint: 64 bytes (8 receiver + 56 args)
TEXT ·executeKernel(SB), NOSPLIT, $128-64
	MOVQ ownerFrame+8(FP), DI
	XORQ R11, R11 // pc = 0
	XORQ R12, R12 // bQueueIdx = 0

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

	// mask = ownerFrame[maskStart]
	MOVQ R14, AX; SHRQ $46, AX; ANDQ $0x7F, AX; MOVQ (DI)(AX*8), CX; MOVQ CX, 48(SP)

	// emit flag
	MOVQ R14, AX; SHRQ $54, AX; ANDQ $1, AX; MOVQ AX, 112(SP)

	// targetB flag
	MOVQ R14, AX; SHRQ $53, AX; ANDQ $1, AX; MOVQ AX, R15

	// topology 
	MOVQ R14, AX; SHRQ $55, AX; ANDQ $3, AX

	// --- 2. TOPOLOGY ROUTING ---
	MOVQ DI, R13 // ptrB defaults to ownerFrame
	MOVQ communitySize+48(FP), R9
	MOVQ community_ptr+24(FP), SI

	CMPQ AX, $1; JEQ topo_pop
	CMPQ AX, $2; JEQ topo_hyper
	JMP topo_done

topo_pop:
	CMPQ R12, R9; JAE topo_done
	MOVQ (SI)(R12*8), R13
	INCQ R12
	JMP topo_done

topo_hyper:
	MOVQ dimCount+56(FP), R10
	TESTQ R10, R10; JZ topo_done

	// pc % dimCount
	MOVQ R11, AX
	XORQ DX, DX
	DIVQ R10 

	MOVQ $1, CX
	SHLQ DX, CX // CX = 1 << dim
	MOVQ ownerIdx+16(FP), BX
	XORQ CX, BX // BX = peerIdx

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
	MOVQ DI, 88(SP)  // ptrA

	MOVQ DI, CX
	CMPQ R15, $1; JNE ptrdst_ok
	MOVQ R13, CX
ptrdst_ok:
	MOVQ CX, 104(SP) // ptrDst

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
	CMPQ 112(SP), $1; JNE next_pc
	MOVQ 48(SP), AX
	TESTQ AX, AX; JZ next_pc
	MOVQ ownerFrame+8(FP), DI
	INCQ 560(DI) // ownerFrame[70] += 1

next_pc:
	MOVQ ownerFrame+8(FP), DI // restore ownerFrame
	INCQ R11
	JMP pc_loop

end_pc_loop:
	RET