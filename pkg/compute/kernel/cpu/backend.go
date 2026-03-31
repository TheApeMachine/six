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

	pi := uint64(core.Cfg.ProgramIndex)
	pcIdx := core.Cfg.RegPC
	if pcIdx == 0 && core.Cfg.ProgramIndex > 0 {
		pcIdx = core.Cfg.ProgramIndex - 1
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
					ctx, instr, pcReg, regs, pc,
				)
			case 2, 0: // ALU & EXT ALU (cls=0)
				pc = backend.handleAlu(
					ctx, instr, pcReg, regs, scratch, cls, pi, pc,
				)
			case 3: // CTL
				pc = backend.handleCtl(
					ctx, instr, pcReg, regs, pc,
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
	regs [4]uint64,
	pc uint64,
) (npc uint64) {
	ctx[0][pcReg&127] = pc + 1
	dir := (instr >> 13) & 1
	reg := (instr >> 11) & 3
	sub := (instr >> 10) & 1

	if dir == 0 { // LOAD: ctx in sub bit
		regs[reg] = ctx[sub][instr&0x7F]
	} else if sub == 0 { // STORE
		ctx[0][instr&0x7F] = regs[reg]
	} else { // IMM
		regs[reg] = uint64(instr & 0x3FF)
	}

	return pc + 1
}

func (backend *Backend) handleAlu(
	ctx [2]*[128]uint64,
	instr uint16,
	pcReg uint64,
	regs [4]uint64,
	scratch [128]uint64,
	cls uint16,
	pi uint64,
	pc uint64,
) (npc uint64) {
	op := uint8((instr >> 10) & 0xF)

	if cls == 0 {
		op += 16
	}

	reg := (instr >> 8) & 3
	fctx := (instr >> 7) & 1
	fword := (instr & 0x7F)

	// JIT fusion: batch consecutive same-op word-sequential instructions.
	n := uint16(1)
	expectedNext := instr + 1

	for n < 128-fword {
		nextPC := pc + uint64(n)
		nWordIdx := (pi + (nextPC >> 2)) & 127
		nShift := (nextPC & 3) << 4

		if uint16(ctx[0][nWordIdx]>>nShift) != expectedNext {
			break
		}

		n++
		expectedNext++
	}

	if n < 4 {
		ctx[0][fword] = backend.execute(op, regs[reg], ctx[fctx][fword])
		ctx[0][pcReg&127] = pc + 1
		return ctx[0][pcReg&127]
	}

	rVal := regs[reg]

	for k := uint16(0); k < n; k++ {
		scratch[k] = rVal
	}

	if fctx == 1 {
		copy(ctx[0][fword:fword+n], ctx[1][fword:fword+n])
	}

	execWordBlock(ctx[0][fword:fword+n], scratch[:n], op)
	ctx[0][pcReg&127] = pc + uint64(n)

	return ctx[0][pcReg&127]
}

func (backend *Backend) handleCtl(
	ctx [2]*[128]uint64,
	instr uint16,
	pcReg uint64,
	regs [4]uint64,
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
