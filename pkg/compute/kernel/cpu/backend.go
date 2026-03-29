package cpu

import (
	"context"
	"fmt"
	"math/bits"
	"runtime"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
)

type GraphEvent struct {
	Type       string
	NodeID     uint64
	NodeTokens string
	NodeType   string
	FromID     uint64
	ToID       uint64
}

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
	traceEnabled    bool
	observer        kernel.Observer
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

/*
NewBackend returns a CPU Backend.
*/
func NewBackend(opts ...backendOption) *Backend {
	backend := &Backend{
		batchCap:        max(2, runtime.NumCPU()-1),
		nextID:          1,
		useAffinityMode: true,
		observer:        kernel.NoopObserver{},
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

// BackendWithTraceEnabled enables the verbose UniversalBitwise trace path.
// It is disabled by default because it is far too expensive for normal runs.
func BackendWithTraceEnabled(enabled bool) backendOption {
	return func(backend *Backend) {
		backend.traceEnabled = enabled
	}
}

// BackendWithObserver injects a kernel observer used for optional trace/error
// reporting. Pass nil to disable.
func BackendWithObserver(observer kernel.Observer) backendOption {
	return func(backend *Backend) {
		backend.observer = kernel.NormalizeObserver(observer)
	}
}

// SetObserver updates the backend observer at runtime.
func (backend *Backend) SetObserver(observer kernel.Observer) {
	backend.observer = kernel.NormalizeObserver(observer)
}

func (backend *Backend) emitGraph(ev GraphEvent) {
	if backend.graphFn != nil {
		backend.graphFn(ev)
	}
}

func traceBackend(observer kernel.Observer, enabled bool, msg string, keyvals ...any) {
	if !enabled {
		return
	}
	kernel.NormalizeObserver(observer).Trace(msg, keyvals...)
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
func HammingDistance(a, b unsafe.Pointer) int {
	dist := 0
	contexts := [2]*[128]uint64{(*[128]uint64)(a), (*[128]uint64)(b)}

	for i := range 128 {
		// We use a simple bitwise XOR and count the set bits
		// to find the geometric distance between two Values.
		diff := contexts[0][i] ^ contexts[1][i]
		dist += bits.OnesCount64(diff)
	}
	return dist
}

/*
UniversalBitwise is the hardware ALU. The only valid way to execute a
program is by applying the 4 bit truth table directly to the spans.

For reference:

	r0 = context A (0=a, 1=b)
	r1 = start A
	r2 = end A
	r3 = context B (0=a, 1=b)
	r4 = start B
	r5 = end B
	pc = program counter (index of next instruction)
	fw = firmware index (index of next program to execute)

In many cases you would need r0 and r3 to both be a (self).
Keep the following in mind: you select the src and dst bits, you
apply the truth table to the bits, that's basically it.
It is important to mind the bootloader, which should be installed
into all new Values.
*/
// loadFirmware copies a compiled firmware program into the user program
// region (slots 4+) when the firmware register is set and pc is zero.
func loadFirmware(c *[128]uint64, p, w uint64) {
	f := c[uint64(core.Cfg.FW)]
	if f == 0 || int(f) >= len(core.Cfg.Firmware) || c[p] != 0 {
		return
	}
	g := core.Cfg.Firmware[f]
	for i, j := 0, w+4; i < len(g) && int(j) < 128; i, j = i+2, j+1 {
		v := uint64(g[i])
		if i+1 < len(g) {
			v |= uint64(g[i+1]) << 32
		}
		c[j] = v
	}
	c[uint64(core.Cfg.FW)] = 0
}

// fetch returns the next instruction word and the pre-increment pc.
// Returns 0, _, true when execution should halt.
func fetch(c *[128]uint64, p, w uint64) (instr uint32, pc uint64, halt bool, exitCode uint16) {
	pc = c[p]
	j := w + pc/2
	if pc >= uint64(core.Cfg.MaxPC) {
		return 0, pc, true, execExitExhausted
	}
	if int(j) >= 128 {
		return 0, pc, true, execExitBadWord
	}
	instr = uint32(c[j] >> (pc % 2 * 32))
	if instr == 0 {
		return 0, pc, true, execExitHalt
	}
	c[p]++
	return instr, pc, false, 0
}

// decode splits a 32-bit instruction into its opcode and operand fields.
func decode(instr uint32) (op uint8, sc, dc uint16, sSp, dSp bool) {
	op = uint8(instr & 0xF)
	sc, dc = uint16((instr>>4)&0x3FFF), uint16((instr>>18)&0x3FFF)
	sSp, dSp = sc&0x3F80 == 0x3000, dc&0x3F80 == 0x3000
	return
}

// execSpan applies the 4-bit truth table bit-by-bit across two spans.
func execSpan(c [2]*[128]uint64, op uint8, sc, dc uint16) {
	sB, dB := uint64(sc&0x7F), uint64(dc&0x7F)
	if int(sB)+2 >= 128 || int(dB)+2 >= 128 {
		return
	}
	sL, sS, sE := int(c[0][sB])&1, c[0][sB+1], c[0][sB+2]
	dL, dS, dE := int(c[0][dB])&1, c[0][dB+1], c[0][dB+2]
	if sE <= sS || dE <= dS {
		return
	}
	sN, dN := sE-sS, dE-dS
	limit := min(sN, dN)
	if sN == 1 {
		limit = dN
	}

	m0 := uint64(0) - uint64((op>>3)&1)
	m1 := uint64(0) - uint64((op>>2)&1)
	m2 := uint64(0) - uint64((op>>1)&1)
	m3 := uint64(0) - uint64(op&1)

	if sN == 1 {
		sWord := sS / 64
		sShift := sS % 64
		if sWord >= 128 {
			return
		}
		sb := (c[sL][sWord] >> sShift) & 1
		var left uint64
		if sb == 1 {
			left = ^uint64(0)
		}

		for i := uint64(0); i < limit; {
			di := (dS + i) / 64
			ds := (dS + i) % 64
			if di >= 128 {
				break
			}

			chunk := uint64(64) - ds
			if i+chunk > limit {
				chunk = limit - i
			}

			var mask uint64
			if chunk == 64 {
				mask = ^uint64(0)
			} else {
				mask = (1 << chunk) - 1
			}
			mask <<= ds

			right := c[dL][di]
			res := m0 ^ ((m0 ^ m2) & left) ^ ((m0 ^ m1) & right) ^ ((m0 ^ m1 ^ m2 ^ m3) & (left & right))

			c[dL][di] = (right & ^mask) | (res & mask)
			i += chunk
		}
	} else {
		for i := uint64(0); i < limit; {
			di := (dS + i) / 64
			ds := (dS + i) % 64
			if di >= 128 {
				break
			}

			chunk := uint64(64) - ds
			if i+chunk > limit {
				chunk = limit - i
			}

			var mask uint64
			if chunk == 64 {
				mask = ^uint64(0)
			} else {
				mask = (1 << chunk) - 1
			}
			mask <<= ds

			sBit := sS + i
			si := sBit / 64
			ss := sBit % 64
			if si >= 128 {
				break
			}

			left := c[sL][si] >> ss
			if ss+chunk > 64 && si+1 < 128 {
				left |= c[sL][si+1] << (64 - ss)
			}
			left <<= ds

			right := c[dL][di]
			res := m0 ^ ((m0 ^ m2) & left) ^ ((m0 ^ m1) & right) ^ ((m0 ^ m1 ^ m2 ^ m3) & (left & right))

			c[dL][di] = (right & ^mask) | (res & mask)
			i += chunk
		}
	}
}

// writeReg applies the truth table to a control/register word destination.
func writeReg(c *[128]uint64, op uint8, dc, sc uint16) {
	if idx := uint64(dc & 0x7F); int(idx) < 128 {
		left := uint64(sc)
		if sc&0x3F80 == 0x3000 {
			if src := uint64(sc & 0x7F); int(src) < 128 {
				left = c[src]
			} else {
				return
			}
		}
		right := c[idx]
		m0 := uint64(0) - uint64((op>>3)&1)
		m1 := uint64(0) - uint64((op>>2)&1)
		m2 := uint64(0) - uint64((op>>1)&1)
		m3 := uint64(0) - uint64(op&1)
		c[idx] = m0 ^ ((m0 ^ m2) & left) ^ ((m0 ^ m1) & right) ^ ((m0 ^ m1 ^ m2 ^ m3) & (left & right))
	}
}

// detectLoop tracks hardware loop state on backward PC writes.
func detectLoop(c *[128]uint64, p, pc, le uint64, lp bool) (uint64, bool) {
	n := c[p]
	if !lp && n < pc {
		return pc, true
	}
	if lp && n > le {
		return 0, false
	}
	return le, lp
}

func clearExecExit(c *[128]uint64) {
	if c == nil || execStatusWord >= len(c) {
		return
	}
	c[execStatusWord] &= execStatusMask
}

func markExecExit(c *[128]uint64, code uint16) {
	if c == nil || execStatusWord >= len(c) {
		return
	}
	c[execStatusWord] = (c[execStatusWord] & execStatusMask) | (uint64(code) << execStatusShift)
}

func (k *Backend) UniversalBitwise(a, b unsafe.Pointer, count int) error {
	if a == nil || b == nil {
		return fmt.Errorf("cpu.Backend.UniversalBitwise: nil value pointer")
	}
	p, w := uint64(core.Cfg.RegPC), uint64(core.Cfg.ProgramIndex)

	for i := 0; i < count; i++ {
		curA := unsafe.Pointer(uintptr(a) + uintptr(i)*1024)
		curB := unsafe.Pointer(uintptr(b) + uintptr(i)*1024)
		c := [2]*[128]uint64{(*[128]uint64)(curA), (*[128]uint64)(curB)}

		loadFirmware(c[0], p, w)
		clearExecExit(c[0])

		var le uint64
		lp := false
		for {
			instr, pc, halt, exitCode := fetch(c[0], p, w)
			if halt {
				if exitCode != 0 {
					markExecExit(c[0], exitCode)
				}
				break
			}
			op, sc, dc, sSp, dSp := decode(instr)
			if sSp && dSp {
				execSpan(c, op, sc, dc)
			} else if dSp {
				writeReg(c[0], op, dc, sc)
			}
			le, lp = detectLoop(c[0], p, pc, le, lp)
		}
	}
	return nil
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
			lane |= contexts[word+1] << uint(64-shift)
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
