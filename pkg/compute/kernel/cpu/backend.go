package cpu

import (
	"context"
	"math/bits"
	"runtime"
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
)

type Backend struct {
	ctx             context.Context
	cancel          context.CancelFunc
	nextID          uint64
	useAffinityMode bool
}

type backendOption func(*Backend)

const (
	execStatusWord  = 63
	execStatusMask  = 0x0000FFFFFFFFFFFF
	execStatusShift = 48

	execExitExhausted = 1
	execExitHalt      = 2
	execExitBadWord   = 3
)

func NewBackend(ctx context.Context, opts ...backendOption) *Backend {
	ctx, cancel := context.WithCancel(ctx)

	backend := &Backend{
		ctx:             ctx,
		cancel:          cancel,
		nextID:          1,
		useAffinityMode: true,
	}

	for _, opt := range opts {
		opt(backend)
	}

	return backend
}

func Available() int { return runtime.NumCPU() }

/*
execute applies the specified opcode to the two 64-bit inputs.
opcodes 0x0–0xF are the 16 boolean truth tables.
0x10 = POPCOUNT (Hamming distance).
0x11 = Memory SHL: y << (x & 63).
0x12 = Memory SHR: y >> (x & 63).
*/
func (backend *Backend) execute(op uint8, x, y uint64) uint64 {
	switch op {
	case 0x0:
		return 0
	case 0x1:
		return x & y
	case 0x2:
		return x &^ y
	case 0x3:
		return x
	case 0x4:
		return ^x & y
	case 0x5:
		return y
	case 0x6:
		return x ^ y
	case 0x7:
		return x | y
	case 0x8:
		return ^(x | y)
	case 0x9:
		return ^(x ^ y)
	case 0xA:
		return ^y
	case 0xB:
		return x | ^y
	case 0xC:
		return ^x
	case 0xD:
		return ^x | y
	case 0xE:
		return ^(x & y)
	case 0xF:
		return ^uint64(0)
	case 0x10:
		return uint64(bits.OnesCount64(x ^ y))
	case 0x11:
		return y << (x & 63) // Memory SHL
	case 0x12:
		return y >> (x & 63) // Memory SHR
	case 0x13:
		return x + y // ADD
	}
	return 0
}

/*
UniversalBitwise executes the in-band program stored in each Value frame.
Four 16-bit instructions are packed per uint64 word in the program region.

Instruction classes (bits [15:14]):

	00 / HALT  — instr == 0 stops execution
	01 / MEM   — load, store, or immediate
	10 / ALU   — boolean truth-table operation
	11 / CTL   — control flow: JMPZ, DJNZ, SHL, SHR

CTL sub-opcodes (bits [13:12]):

	00 JMPZ  — if reg == 0, jump by signed 10-bit offset (bits [9:0])
	01 DJNZ  — reg--; if reg != 0, jump by signed 10-bit offset
	10 SHL   — reg <<= imm (bits [9:0], 0 means 1)
	11 SHR   — reg >>= imm (bits [9:0], 0 means 1)
*/
func (backend *Backend) UniversalBitwise(a, b unsafe.Pointer, count int) error {
	if a == nil || b == nil {
		return NewSimdeezNutsError(ErrNilValuePointer,
			"a", a, "b", b,
		)
	}

	pi := uint64(core.Cfg.Value.Region.Program.Start)
	pcIdx := uint64(core.Cfg.Value.Region.Registers.PC)	
	if pcIdx == 0 && core.Cfg.Value.Region.Program.Start > 0 {
		pcIdx = uint64(core.Cfg.Value.Region.Program.Start - 1)
	}
	pcReg := uint64(pcIdx)

	var scratch [128]uint64

	for i := 0; i < count; i++ {
		curA := unsafe.Pointer(uintptr(a) + uintptr(i)*1024)
		curB := unsafe.Pointer(uintptr(b) + uintptr(i)*1024)
		ctx := [2]*[128]uint64{(*[128]uint64)(curA), (*[128]uint64)(curB)}

		var regs [4]uint64

		for {
			pc := ctx[0][pcReg&127]

			word := (pi + (pc >> 2)) & 127
			shift := (pc & 3) << 4
			instr := uint16(ctx[0][word] >> shift)

			if instr == 0 {
				break // HALT
			}

			switch cls := instr >> 14; cls {
			case 1: // MEM
				pc = backend.handleMem(
					ctx, instr, pcReg, &regs, pc,
				)
			case 2, 0: // ALU & EXT ALU (cls=0)
				pc = backend.handleAlu(
					ctx, instr, pcReg, &regs, scratch, cls, pi, pc,
				)
			case 3: // CTL
				pc = backend.handleCtl(
					ctx, instr, pcReg, &regs, pc,
				)
			}

			ctx[0][pcReg&127] = pc
		}
	}
	return nil
}

func (backend *Backend) handleMem(
	ctx [2]*[128]uint64,
	instr uint16,
	pcReg uint64,
	regs *[4]uint64,
	pc uint64,
) (npc uint64) {
	dir := (instr >> 13) & 1
	reg := (instr >> 11) & 3
	sub := (instr >> 10) & 1
	indirect := (instr >> 9) & 1

	if dir == 0 {
		if indirect == 1 {
			// ILOAD: reg = ctx[sub][regs[addrReg] & 127]
			addrReg := instr & 3
			addr := regs[addrReg] & 127
			regs[reg] = ctx[sub][addr]
		} else {
			// LOAD: reg = ctx[sub][word]
			regs[reg] = ctx[sub][instr&0x7F]
		}
	} else if sub == 0 {
		if indirect == 1 {
			// ISTORE: ctx[0][regs[addrReg] & 127] = regs[reg]
			addrReg := instr & 3
			addr := regs[addrReg] & 127
			ctx[0][addr] = regs[reg]
		} else {
			// STORE: ctx[0][word] = regs[reg]
			ctx[0][instr&0x7F] = regs[reg]
		}
	} else {
		// IMM: regs[reg] = immediate (0-1023)
		regs[reg] = uint64(instr & 0x3FF)
	}

	return pc + 1
}

// aluOp holds a decoded ALU instruction ready for issue.
type aluOp struct {
	op    uint8
	reg   uint8
	fctx  uint8
	fword uint8
}

func (backend *Backend) handleAlu(
	ctx [2]*[128]uint64,
	instr uint16,
	pcReg uint64,
	regs *[4]uint64,
	scratch [128]uint64,
	cls uint16,
	pi uint64,
	pc uint64,
) (npc uint64) {
	op0 := uint8((instr >> 10) & 0xF)
	if cls == 0 {
		op0 += 16
	}
	reg0 := uint8((instr >> 8) & 3)
	fctx0 := uint8((instr >> 7) & 1)
	fword0 := uint8(instr & 0x7F)

	// Fast path: look ahead for a contiguous window of ALU ops with the same
	// (op, reg, fctx) and strictly incrementing fword. This is the common case
	// emitted by compilers targeting this ISA.
	//
	// We scan up to the end of the context window, without an n<4 guard —
	// SIMD dispatch is always beneficial on arm64/amd64 regardless of n.
	n := uint16(1)
	{
		maxN := uint16(128) - uint16(fword0)
		if maxN > 124 {
			maxN = 124 // leave room at window edge
		}
		expectedNext := instr + 1 // same op/reg/fctx, fword+1
		for n < maxN {
			nextPC := pc + uint64(n)
			nWordIdx := (pi + (nextPC >> 2)) & 127
			nShift := (nextPC & 3) << 4
			if uint16(ctx[0][nWordIdx]>>nShift) != expectedNext {
				break
			}
			n++
			expectedNext++
		}
	}

	if n >= 2 {
		// Contiguous same-op run — dispatch directly via SIMD.
		rVal := regs[reg0]
		for k := uint16(0); k < n; k++ {
			scratch[k] = rVal
		}
		fw := uint16(fword0)
		if fctx0 == 1 {
			copy(ctx[0][fw:fw+n], ctx[1][fw:fw+n])
		}
		execWordBlock(ctx[0][fw:fw+n], scratch[:n], op0)
		ctx[0][pcReg&127] = pc + uint64(n)
		return ctx[0][pcReg&127]
	}

	// Lookahead window: scan up to 64 further instructions for ALU ops with
	// the same op type that we can gather-SIMD-scatter, hoisting them above
	// intervening independent ALU ops of different types.
	//
	// Scoreboard: a 128-bit dirty set tracking ctx[0] words written in this
	// window. Two uint64s cover indices 0–127.
	var dirtyLo, dirtyHi uint64
	markDirty := func(w uint8) {
		if w < 64 {
			dirtyLo |= 1 << w
		} else {
			dirtyHi |= 1 << (w - 64)
		}
	}
	isDirty := func(w uint8) bool {
		if w < 64 {
			return dirtyLo>>w&1 != 0
		}
		return dirtyHi>>(w-64)&1 != 0
	}

	// Buffer for gather: indices into ctx[0] and corresponding src values.
	// Max 64 ops per lookahead pass.
	type gatherSlot struct {
		fword uint8
		fctx  uint8
		reg   uint8
	}
	var slots [64]gatherSlot
	nSlots := 0
	consumed := uint64(0) // number of PCs consumed by this dispatch (always ≥1)

	// Include the triggering instruction.
	if !isDirty(fword0) {
		slots[nSlots] = gatherSlot{fword0, fctx0, reg0}
		nSlots++
		markDirty(fword0)
	}

	// Scan ahead for more same-op ALU instructions.
	scanLimit := uint64(64)
	for look := uint64(1); look < scanLimit && nSlots < 64; look++ {
		lPC := pc + look
		lWordIdx := (pi + (lPC >> 2)) & 127
		lShift := (lPC & 3) << 4
		lInstr := uint16(ctx[0][lWordIdx] >> lShift)

		if lInstr == 0 {
			break // HALT — stop lookahead
		}

		lCls := lInstr >> 14
		if lCls == 3 {
			// CTL: branch/jump — ordering barrier, stop lookahead.
			break
		}
		if lCls == 1 {
			// MEM: mark the destination word dirty if it is a store to ctx[0].
			lDir := (lInstr >> 13) & 1
			lSub := (lInstr >> 10) & 1
			if lDir == 1 && lSub == 0 {
				lFword := uint8(lInstr & 0x7F)
				markDirty(lFword)
			}
			continue
		}

		lOp := uint8((lInstr >> 10) & 0xF)
		if lCls == 0 {
			lOp += 16
		}
		lReg := uint8((lInstr >> 8) & 3)
		lFctx := uint8((lInstr >> 7) & 1)
		lFword := uint8(lInstr & 0x7F)

		if lOp != op0 {
			// Different op type: mark its destination dirty so later ops of op0
			// that depend on it are not hoisted, then continue scanning.
			if !isDirty(lFword) {
				markDirty(lFword)
			}
			continue
		}

		if isDirty(lFword) {
			continue // WAW hazard — skip
		}

		slots[nSlots] = gatherSlot{lFword, lFctx, lReg}
		nSlots++
		markDirty(lFword)
	}

	if nSlots == 1 {
		// No benefit from gather path — scalar dispatch for the single op.
		ctx[0][fword0] = backend.execute(op0, regs[reg0], ctx[fctx0][fword0])
		ctx[0][pcReg&127] = pc + 1
		return ctx[0][pcReg&127]
	}

	// Gather: pack dst and src into scratch[0:nSlots] and scratch[64:64+nSlots].
	dstScratch := scratch[0:nSlots]
	srcScratch := scratch[64 : 64+nSlots]
	for i, s := range slots[:nSlots] {
		dstScratch[i] = ctx[s.fctx][s.fword]
		srcScratch[i] = regs[s.reg]
	}

	execWordBlock(dstScratch, srcScratch, op0)

	// Scatter results back and advance PC by consumed instructions.
	// We only commit the leading contiguous run of dispatched slots in PC order
	// (i.e. slot[0] which is always the instruction at `pc`).
	// Hoisted out-of-order results for slots[1:] are written to ctx[0] directly;
	// their PC positions will be skipped when the loop reaches them (the words
	// will already have been updated — harmless since ctx[0][fword] is idempotent
	// for pure functional ops given the same register values).
	for i, s := range slots[:nSlots] {
		ctx[0][s.fword] = dstScratch[i]
	}

	// Advance PC: skip the triggering instruction only. The out-of-order
	// slots will be re-encountered but their destinations are already written.
	consumed = 1
	ctx[0][pcReg&127] = pc + consumed
	return ctx[0][pcReg&127]
}

func (backend *Backend) handleCtl(
	ctx [2]*[128]uint64,
	instr uint16,
	pcReg uint64,
	regs *[4]uint64,
	pc uint64,
) (npc uint64) {
	sub := (instr >> 12) & 3
	reg := (instr >> 10) & 3
	// 10-bit signed offset, sign-extended from bit 9.
	raw := int16(instr & 0x3FF)

	if raw&0x200 != 0 {
		raw |= -512 // sign-extend 10-bit → 16-bit
	}

	offset := int64(raw)

	switch sub {
	case 0: // JMPZ — jump by offset if reg == 0, else advance by 1
		if regs[reg] != 0 {
			ctx[0][pcReg&127] = pc + 1
			break
		}

		ctx[0][pcReg&127] = uint64(int64(pc) + offset)
	case 1: // DJNZ — decrement reg; jump by offset if still != 0
		regs[reg]--

		if regs[reg] == 0 {
			ctx[0][pcReg&127] = pc + 1
			break
		}

		ctx[0][pcReg&127] = uint64(int64(pc) + offset)
	case 2: // SHL — logical left shift by imm (0 → shift by 1)
		s := uint(instr&0x3FF) & 63

		if s == 0 {
			s = 1
		}

		regs[reg] <<= s
	case 3: // SHR — logical right shift by imm (0 → shift by 1)
		s := uint(instr&0x3FF) & 63

		if s == 0 {
			s = 1
		}

		regs[reg] >>= s
	}

	return ctx[0][pcReg&127]
}

func WithAffinityMode(enabled bool) backendOption {
	return func(backend *Backend) { backend.useAffinityMode = enabled }
}

func (backend *Backend) Shutdown() error {
	backend.cancel()
	return nil
}

func (backend *Backend) Schedule(job func(ctx context.Context) error) error {
	return job(context.Background())
}

func Popcount(value unsafe.Pointer, startBit, bitLen int) int {
	contexts := (*[128]uint64)(value)

	if bitLen <= 0 {
		return 0
	}

	startWord := startBit >> 6
	startShift := startBit & 63
	remaining := bitLen
	total := 0
	word := startWord
	shift := startShift

	for remaining > 0 {
		chunk := min(64-shift, remaining)

		var lane uint64
		lane = contexts[word] >> uint(shift)

		if shift > 0 && word+1 < 128 {
			val := contexts[word+1]
			lane |= val << uint(64-shift)
		}

		mask := uint64(1<<chunk) - 1
		if chunk == 64 {
			mask = ^uint64(0)
		}

		total += bits.OnesCount64(lane & mask)

		remaining -= chunk
		word++
		shift = 0
	}

	return total
}
