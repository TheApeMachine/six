package cpu

import (
	"context"
	"math/bits"
	"runtime"
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
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

// BackendWithAffinityMode enables the new in-band affinity + program execution mode.
// This is the gradual migration path - old behavior remains the default.
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
UniversalBitwise is the pure hardware ALU. Each Value's in-band program
is executed through the 16 two-input boolean truth tables.
*/
/*
UniversalBitwise is the hardware ALU. Each Value executes its in-band
program across contexts A and B using the 7-register RISC model.
*/
func (backend *Backend) UniversalBitwise(
	a, b, _ unsafe.Pointer, numValues uint32,
) error {
	for v := uint32(0); v < numValues; v++ {
		backend.executeProgram(a, b, v)
	}
	return nil
}

func (backend *Backend) executeProgram(a, b unsafe.Pointer, v uint32) {
	as := unsafe.Slice((*primitive.Value)(a), v+1)
	bs := unsafe.Slice((*primitive.Value)(b), v+1)
	valA := &as[v]
	valB := &bs[v]
	contexts := []*primitive.Value{valA, valB}

	if backend.graphFn != nil {
		backend.emitGraph(GraphEvent{
			Type:       "add-node",
			NodeID:     valA.ValueID(),
			NodeTokens: primitive.DecodeTokensToText(valA),
			NodeType:   "Context A",
		})
		backend.emitGraph(GraphEvent{
			Type:       "add-node",
			NodeID:     valB.ValueID(),
			NodeTokens: primitive.DecodeTokensToText(valB),
			NodeType:   "Context B",
		})
		backend.emitGraph(GraphEvent{
			Type:       "add-edge",
			FromID:     valB.ValueID(),
			ToID:       valA.ValueID(),
		})
	}

	pcIdx := uint64(core.Cfg.RegPC)
	wordBase := uint64(core.Cfg.ProgramIndex) / 64

	// If firmware register (FW) is set, initialize the program region.
	fwIdx := valA[core.Cfg.FW]
	if fwIdx > 0 && int(fwIdx) < len(core.Cfg.Firmware) {
		bootloaderIdx, ok := core.Cfg.FirmwareIndex["bootloader"]
		if ok && bootloaderIdx >= 0 && bootloaderIdx < len(core.Cfg.Firmware) {
			copyProgram(valA, core.Cfg.Firmware[bootloaderIdx], 0)
		}
		copyProgram(valA, core.Cfg.Firmware[fwIdx], 8)
		valA[core.Cfg.FW] = 0 // Clear the trigger
	}

	for {
		pc := valA[pcIdx]
		if pc >= uint64(core.Cfg.MaxPC) {
			break
		}

		wordPos := wordBase + (pc / 2)
		shift := uint((pc % 2) * 32)
		instr := uint32(valA[wordPos] >> shift)

		op := uint8(instr & 0xF)
		if op == 0 && pc > 0 {
			break
		}

		srcCode := uint16((instr >> 4) & 0x3FFF)
		dstCode := uint16((instr >> 18) & 0x3FFF)

		valA[pcIdx]++

		resolve := func(code uint16) (uint64, bool) {
			if code&0x1000 != 0 {
				return uint64(code & 0x0FFF), true
			}
			if code&0x2000 != 0 {
				return valA[code&0x0FFF], false
			}
			return uint64(code), false
		}

		srcVal, sSpan := resolve(srcCode)
		dstVal, dSpan := resolve(dstCode)

		if sSpan || dSpan {
			sBase := srcVal
			sCtx, sOff, sLen := valA[sBase], valA[sBase+1], valA[sBase+2]

			dBase := dstVal
			dCtx, dOff, dLen := valA[dBase], valA[dBase+1], valA[dBase+2]

			limit := min(sLen, dLen)
			for i := uint64(0); i < limit; i++ {
				sb := getBit(contexts[sCtx%2], sOff+i)
				db := getBit(contexts[dCtx%2], dOff+i)
				// Reverse binary order: (1,1)=0, (1,0)=1, (0,1)=2, (0,0)=3
				idx := (1 - db) | ((1 - sb) << 1)
				res := (op >> idx) & 1
				setBit(contexts[dCtx%2], dOff+i, res)
			}
			continue
		}

		dstIdx := int(dstCode & 0x0FFF)
		left := srcVal
		right := dstVal

		// m0-m3 mapped to bits 3-0
		m0 := uint64(0) - uint64((op>>3)&1) // bit 3 = f(0,0)
		m1 := uint64(0) - uint64((op>>2)&1) // bit 2 = f(0,1)
		m2 := uint64(0) - uint64((op>>1)&1) // bit 1 = f(1,0)
		m3 := uint64(0) - uint64(op&1)      // bit 0 = f(1,1)

		valA[dstIdx] = m0 ^ ((m0 ^ m2) & left) ^ ((m0 ^ m1) & right) ^ ((m0 ^ m1 ^ m2 ^ m3) & (left & right))
	}
}

func copyProgram(v *primitive.Value, instrs []uint32, startSlot int) {
	wordBase := uint64(core.Cfg.ProgramIndex) / 64
	for i, inst := range instrs {
		slot := startSlot + i
		wordPos := wordBase + uint64(slot/2)
		shift := uint((slot % 2) * 32)

		// Clear the 32-bit slot
		v[wordPos] &^= (uint64(0xFFFFFFFF) << shift)
		// Set the instruction
		v[wordPos] |= (uint64(inst) << shift)
	}
}

func getBit(v *primitive.Value, pos uint64) uint8 {
	word := pos >> 6
	bit := pos & 63
	if word >= primitive.Words {
		return 0
	}
	return uint8((v[word] >> bit) & 1)
}

func setBit(v *primitive.Value, pos uint64, val uint8) {
	word := pos >> 6
	bit := pos & 63
	if word >= primitive.Words {
		return
	}
	if val == 1 {
		v[word] |= (1 << bit)
	} else {
		v[word] &= ^(1 << bit)
	}
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

func (backend *Backend) Schedule(job func(ctx context.Context) error) {
	_ = job(context.Background())
}
