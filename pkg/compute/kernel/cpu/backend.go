package cpu

import (
	"context"
	"fmt"
	"math/bits"
	"runtime"
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

type GraphEvent struct {
	Type       string
	NodeID     uint64
	NodeTokens string
	NodeType   string
	FromID     uint64
	ToID       uint64
}

const InstructionByteMask byte = 0x80 // 10000000 in binary

/*
Backend is the CPU kernel backend. It is now a dumb physics engine
that performs in-band affinity-based pairing and program execution.

The old "residents" array, cancellation logic, and Go-level orchestration
have been removed. Values now carry their own programs in Region 3 and
affinity masks in Region 2. The backend simply streams Values through
the UniversalBitwise ALU.
*/
type Backend struct {
	batchCap        int
	nextID          uint64
	graphFn         func(GraphEvent)
	useAffinityMode bool
}

type backendOption func(*Backend)

/*
NewBackend returns a CPU Backend.
*/
func NewBackend(opts ...backendOption) *Backend {
	backend := &Backend{
		batchCap:        max(2, runtime.NumCPU()-1),
		nextID:          1,
		useAffinityMode: true,
	}

	for _, opt := range opts {
		opt(backend)
	}

	if backend.batchCap < 2 {
		backend.batchCap = 2
	}

	return backend
}

func BackendWithBatchCap(batchCap int) backendOption {
	return func(backend *Backend) {
		backend.batchCap = batchCap
	}
}

// BackendWithGraphHook attaches a callback invoked during splits so an
// external visualizer can render the graph structure in real time.
func BackendWithGraphHook(fn func(GraphEvent)) backendOption {
	return func(backend *Backend) {
		backend.graphFn = fn
	}
}

// BackendWithAffinityMode toggles in-band affinity + program execution mode.
// NewBackend enables this mode by default; pass false to disable (legacy behavior).
func BackendWithAffinityMode(enabled bool) backendOption {
	return func(backend *Backend) {
		backend.useAffinityMode = enabled
	}
}

func (backend *Backend) emitGraph(ev GraphEvent) {
	if backend.graphFn != nil {
		backend.graphFn(ev)
	}
}

/*
Available returns the number of logical CPU cores.
*/
func Available() int {
	return runtime.NumCPU()
}

/*
HammingDistance calculates the topological distance between two Values
by counting the number of differing bits (symmetric difference).
*/
func HammingDistance(a, b *primitive.Value) int {
	dist := 0
	for i := range primitive.Words {
		// We use a simple bitwise XOR and count the set bits
		// to find the geometric distance between two Values.
		diff := a[i] ^ b[i]
		dist += bits.OnesCount64(diff)
	}
	return dist
}

/*
UniversalBitwise is the hardware ALU. The only valid way to execute a
program is by applying the 4 bit truth table directly to the spans.

For reference:

	r0 = context A (0=a, 1=b)
	r1 = offset A
	r2 = length A
	r3 = context B (0=a, 1=b)
	r4 = offset B
	r5 = length B
	pc = program counter (index of next instruction)
	fw = firmware index (index of next program to execute)

In many cases you would need r0 and r3 to both be a (self).
Keep the following in mind: you select the src and dst bits, you
apply the truth table to the bits, that's basically it.
It is important to mind the bootloader, which should be installed
into all new Values.
*/
func (backend *Backend) UniversalBitwise(a, b unsafe.Pointer) error {
	if a == nil || b == nil {
		return fmt.Errorf("cpu.Backend.UniversalBitwise: nil value pointer")
	}
	valA := (*primitive.Value)(a)
	valB := (*primitive.Value)(b)
	contexts := []*primitive.Value{valA, valB}

	pcIdx := uint64(core.Cfg.RegPC)
	wordBase := uint64(core.Cfg.ProgramIndex)

	fwIdx := uint64(core.Cfg.FW)
	fw := valA[fwIdx]

	// In-band firmware loading mechanism
	if fw > 0 && int(fw) < len(core.Cfg.Firmware) && valA[pcIdx] == uint64(0) {
		prog := core.Cfg.Firmware[fw]
		empty := true
		for i := uint64(0); i < uint64(core.Cfg.ProgramBits)/64; i++ {
			if valA[wordBase+i] != 0 {
				empty = false
				break
			}
		}
		if empty {
			for i := 0; i < len(prog); i += 2 {
				wordPos := wordBase + uint64(i/2)
				var w uint64
				w = uint64(prog[i])
				if i+1 < len(prog) {
					w |= uint64(prog[i+1]) << 32
				}
				valA[wordPos] = w
			}
			valA[fwIdx] = 0 // consumed firmware index; avoid re-loading same slot
		}
	}

	valA.ClearExecExitCode()

	for {
		pc := valA[pcIdx]
		errnie.Trace("cpu.Backend.UniversalBitwise", "pc", pc, "maxPC", core.Cfg.MaxPC)

		if pc >= uint64(core.Cfg.MaxPC) {
			valA.SetExecExitCode(primitive.ExecExitExhausted)
			errnie.Trace("cpu.Backend.UniversalBitwise", "pc", pc, "maxPC", core.Cfg.MaxPC)
			break
		}

		wordPos := wordBase + (pc / 2)
		errnie.Trace("cpu.Backend.UniversalBitwise", "wordPos", wordPos, "primitive.Words", primitive.Words)

		if int(wordPos) >= primitive.Words {
			valA.SetExecExitCode(primitive.ExecExitBadProgramWord)
			errnie.Trace("cpu.Backend.UniversalBitwise", "wordPos", wordPos, "primitive.Words", primitive.Words)
			break
		}

		shift := uint((pc % 2) * 32)
		instr := uint32(valA[wordPos] >> shift)

		op := uint8(instr & 0xF)
		errnie.Trace("cpu.Backend.UniversalBitwise", "op", op, "pc", pc)

		if op == 0 && pc > 0 {
			valA.SetExecExitCode(primitive.ExecExitHaltOpcode)
			errnie.Trace("cpu.Backend.UniversalBitwise", "op", op, "pc", pc)
			break
		}

		srcCode := uint16((instr >> 4) & 0x3FFF)
		dstCode := uint16((instr >> 18) & 0x3FFF)
		errnie.Trace("cpu.Backend.UniversalBitwise", "srcCode", srcCode, "dstCode", dstCode)

		valA[pcIdx]++
		errnie.Trace("cpu.Backend.UniversalBitwise", "pcIdx", pcIdx, "valA[pcIdx]", valA[pcIdx])

		resolve := func(code uint16) (uint64, bool) {
			if code&0x1000 != 0 {
				return uint64(code & 0x0FFF), true
			}
			if code&0x2000 != 0 {
				idx := int(code & 0x0FFF)
				if idx >= primitive.Words {
					errnie.Trace("cpu.Backend.UniversalBitwise", "idx", idx, "primitive.Words", primitive.Words)
					return 0, false
				}
				return valA[idx], false
			}
			return uint64(code), false
		}

		srcVal, sSpan := resolve(srcCode)
		dstVal, dSpan := resolve(dstCode)
		errnie.Trace("cpu.Backend.UniversalBitwise", "srcVal", srcVal, "dstVal", dstVal)

		if sSpan || dSpan {
			sBase := srcVal
			dBase := dstVal

			if int(sBase)+2 >= primitive.Words || int(dBase)+2 >= primitive.Words {
				continue
			}

			sCtx, sOff, sLen := valA[sBase], valA[sBase+1], valA[sBase+2]

			dCtx, dOff, dLen := valA[dBase], valA[dBase+1], valA[dBase+2]

			limit := min(sLen, dLen)

			errnie.Trace(
				"cpu.Backend.UniversalBitwise",
				"limit", limit,
				"sLen", sLen,
				"dLen", dLen,
				"sBase", sBase,
				"dBase", dBase,
			)

			if limit > 0 {
				sLast := (sOff + limit - 1) / 64
				dLast := (dOff + limit - 1) / 64
				if sLast >= uint64(primitive.Words) || dLast >= uint64(primitive.Words) {
					errnie.Trace(
						"cpu.Backend.UniversalBitwise",
						"sLast", sLast,
						"dLast", dLast,
						"primitive.Words", primitive.Words,
					)
					return fmt.Errorf(
						"cpu.Backend.UniversalBitwise: span exceeds value (%d words): sLast=%d dLast=%d",
						primitive.Words, sLast, dLast,
					)
				}
			}

			// This is the "Hardware Span" execution (Bootloader/Affinity)
			// We treat the context as a raw bit-array
			for itr := uint64(0); itr < limit; itr++ {
				// RAW BIT READ (Manual extraction)
				sIdx, sShift := (sOff+itr)/64, (sOff+itr)%64
				dIdx, dShift := (dOff+itr)/64, (dOff+itr)%64
				if sIdx >= uint64(primitive.Words) || dIdx >= uint64(primitive.Words) {
					break
				}
				sLane := int(sCtx) & 1
				dLane := int(dCtx) & 1

				sb := (contexts[sLane][sIdx] >> sShift) & 1
				db := (contexts[dLane][dIdx] >> dShift) & 1

				errnie.Trace(
					"cpu.Backend.UniversalBitwise",
					"sIdx", sIdx,
					"dIdx", dIdx,
					"sLane", sLane,
					"dLane", dLane,
				)

				// UNIVERSAL ALU LOGIC (Algebraic Normal Form)
				// Maps (sb,db) to the truth table result
				idx := (1 - db) | ((1 - sb) << 1)
				res := (op >> idx) & 1

				// RAW BIT WRITE (Manual insertion)
				target := &contexts[dLane][dIdx]
				*target = (*target & ^(uint64(1) << dShift)) | (uint64(res) << dShift)
			}

			continue
		}

		dstIdx := int(dstCode & 0x0FFF)
		if dstIdx >= primitive.Words {
			continue
		}
		left := srcVal
		right := dstVal

		// m0-m3 mapped to bits 3-0
		m0 := uint64(0) - uint64((op>>3)&1) // bit 3 = f(0,0)
		m1 := uint64(0) - uint64((op>>2)&1) // bit 2 = f(0,1)
		m2 := uint64(0) - uint64((op>>1)&1) // bit 1 = f(1,0)
		m3 := uint64(0) - uint64(op&1)      // bit 0 = f(1,1)

		valA[dstIdx] = m0 ^ ((m0 ^ m2) & left) ^ ((m0 ^ m1) & right) ^ ((m0 ^ m1 ^ m2 ^ m3) & (left & right))
	}

	return nil
}

func Popcount(value *primitive.Value, startBit, bitLen int) int {
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
		lane = value[word] >> uint(shift)

		if shift > 0 && word+1 < primitive.Words {
			lane |= value[word+1] << uint(64-shift)
		}

		if chunk < 64 {
			lane &= (uint64(1) << uint(chunk)) - 1
		}

		total += bits.OnesCount64(lane)
		remaining -= chunk
		word++
		shift = 0
	}

	return total
}

func (backend *Backend) Schedule(job func(ctx context.Context) error) error {
	return job(context.Background())
}
