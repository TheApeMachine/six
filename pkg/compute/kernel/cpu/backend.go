package cpu

import (
	"bytes"
	"fmt"
	"io"
	"math/bits"
	"runtime"
	"sync"
	"unsafe"

	"github.com/smallnest/ringbuffer"
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
	pr                *ringbuffer.PipeReader
	pw                *ringbuffer.PipeWriter
	rb                *ringbuffer.RingBuffer
	outPr             *ringbuffer.PipeReader
	outPw             *ringbuffer.PipeWriter
	outRb             *ringbuffer.RingBuffer
	batchCap          int
	streamPassthrough bool
	streamOutMu       sync.Mutex
	streamOut         bytes.Buffer // passthrough output (avoids ring deadlock with synchronous Write→Read pipelines)
	nextID            uint64
	graphFn           func(GraphEvent)
	useAffinityMode   bool // enables in-band affinity + program execution (replaces residents list)
}

type backendOption func(*Backend)

/*
NewBackend returns a CPU Backend.
*/
func NewBackend(opts ...backendOption) *Backend {
	backend := &Backend{
		batchCap:        max(2, runtime.NumCPU()-1),
		nextID:          1,
		useAffinityMode: true, // default: use in-band affinity + program execution (no residents list)
	}

	for _, opt := range opts {
		opt(backend)
	}

	if backend.batchCap < 2 {
		backend.batchCap = 2
	}

	rb := ringbuffer.New(backend.batchCap * primitive.ByteSize)
	pr, pw := rb.Pipe()
	outRb := ringbuffer.New(3 * backend.batchCap * primitive.ByteSize)
	outPr, outPw := outRb.Pipe()

	backend.pr = pr
	backend.pw = pw
	backend.rb = rb
	backend.outPr = outPr
	backend.outPw = outPw
	backend.outRb = outRb
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

// BackendWithStreamPassthrough enables single-frame (and sub-batchCap) processing
// for io pipelines that emit one Value at a time. When no cancellation pair exists,
// frames are passed through to Read unchanged. Default backend batches batchCap
// frames before any output (see tests).
func BackendWithStreamPassthrough() backendOption {
	return func(backend *Backend) {
		backend.streamPassthrough = true
	}
}

/*
Available returns the number of logical CPU cores.
*/
func Available() int {
	return runtime.NumCPU()
}

func (backend *Backend) Read(p []byte) (n int, err error) {
	if backend.streamPassthrough {
		backend.streamOutMu.Lock()
		defer backend.streamOutMu.Unlock()
		if backend.streamOut.Len() == 0 {
			return 0, io.EOF
		}
		return backend.streamOut.Read(p)
	}

	if backend.outRb.Length() == 0 {
		return 0, io.EOF
	}

	n, err = backend.outPr.Read(p)

	if err != nil && err != io.EOF {
		errnie.Error(err)
		return 0, err
	}

	if n == 0 {
		return 0, io.EOF
	}

	return n, err
}

func (backend *Backend) emitOutputFrame(frame []byte) error {
	if backend.streamPassthrough {
		backend.streamOutMu.Lock()
		_, err := backend.streamOut.Write(frame)
		backend.streamOutMu.Unlock()
		return err
	}
	_, err := backend.outPw.Write(frame)
	return err
}

func (backend *Backend) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	// copyChain uses a large buffer; pw.Write blocks when the ring is full until a
	// reader drains, but draining only happens in processAvailableBatch on this
	// goroutine. Chunk by Free() and drain between chunks so we never block inside
	// pw.Write with no other code able to call processAvailableBatch.
	total := 0

	for len(p) > 0 {
		if err := backend.processAvailableBatch(); err != nil {
			return total, err
		}
		free := backend.rb.Free()
		if free == 0 {
			return total, fmt.Errorf("cpu.backend: input ring full after drain (len=%d cap=%d)", backend.rb.Length(), backend.rb.Capacity())
		}
		chunk := p
		if len(chunk) > free {
			chunk = p[:free]
		}
		var nw int
		nw, err = backend.pw.Write(chunk)
		total += nw
		if errnie.Error(err) != nil {
			return total, err
		}
		if nw == 0 && len(p) > 0 {
			return total, io.ErrShortWrite
		}
		p = p[nw:]
		if err := backend.processAvailableBatch(); err != nil {
			return total, err
		}
	}
	return total, nil
}

func (backend *Backend) Close() error {
	return nil
}

func (backend *Backend) processAvailableBatch() error {
	if backend.streamPassthrough {
		return backend.processStreamBatches()
	}
	return backend.processFullBatch()
}

func (backend *Backend) processFullBatch() error {
	batchBytes := backend.batchCap * primitive.ByteSize
	if batchBytes <= 0 || backend.rb.Length() < batchBytes {
		return nil
	}

	buf := make([]byte, batchBytes)
	if _, err := io.ReadFull(backend.pr, buf); err != nil {
		return err
	}

	batch := decodeBatchFrames(buf)

	// Initialize affinity masks for all Values in this batch.
	// This enables the topological clustering used by AffinityMatch.
	for i := range batch {
		batch[i].InitializeAffinity()
	}

	// NEW PATH: Use affinity-based pairing + program execution
	// The old residents/cancellation path has been removed.
	if err := backend.processAffinityBatch(batch); err != nil {
		return err
	}
	return nil
}

// processAffinityBatch implements the new in-band affinity + program execution path.
// It finds Values with high affinity and runs their programs through UniversalBitwise.
func (backend *Backend) processAffinityBatch(batch []primitive.Value) error {
	if len(batch) == 0 {
		return nil
	}

	// Use affinity-based pairing to find good topological matches
	for i := range batch {
		v := &batch[i]

		// Find the best match for this Value using affinity.
		// bestMatch is only set when a genuine partner is found;
		// matched == false means no other Value in the batch had
		// sufficient affinity overlap.
		bestMatch := -1
		for j := range batch {
			if i == j {
				continue
			}
			if AffinityMatch(v, &batch[j], 2) { // threshold of 2 overlapping bits
				bestMatch = j
				break // take the first good match for now
			}
		}

		emitted := primitive.NewValue()

		if bestMatch == -1 {
			// No affinity partner found — pass the value through unchanged
			// rather than running UniversalBitwise against itself.
			*emitted = *v
		} else {
			target := &batch[bestMatch]
			if err := backend.UniversalBitwise(
				unsafe.Pointer(v),
				unsafe.Pointer(target),
				unsafe.Pointer(emitted),
				1,
			); err != nil {
				return err
			}
		}

		// Set metadata
		emitted.SetValueID(backend.nextID)
		backend.nextID++
		emitted[core.Cfg.StateIndex] = 1

		// Preserve linking information if present.
		// When a real match was found, prefer the target's link chain;
		// otherwise fall back to v's own link (or none).
		if bestMatch != -1 && batch[bestMatch].NextValueID() != 0 {
			emitted.SetNextValueID(batch[bestMatch].NextValueID())
		} else if v.NextValueID() != 0 {
			emitted.SetNextValueID(v.NextValueID())
		}

		if err := backend.emitOutputFrameFromValue(emitted); err != nil {
			return err
		}
	}
	return nil
}

// emitOutputFrameFromValue is a helper to emit a Value through the output pipeline.
func (backend *Backend) emitOutputFrameFromValue(v *primitive.Value) error {
	frame := make([]byte, primitive.ByteSize)
	if err := primitive.ValueToBytes(v, frame); err != nil {
		return err
	}
	return backend.emitOutputFrame(frame)
}

func (backend *Backend) processStreamBatches() error {
	frameSize := primitive.ByteSize
	for backend.rb.Length() >= frameSize {
		nFrames := min(backend.rb.Length()/frameSize, backend.batchCap)
		batchBytes := nFrames * frameSize
		buf := make([]byte, batchBytes)
		if _, err := io.ReadFull(backend.pr, buf); err != nil {
			return err
		}

		batch := decodeBatchFrames(buf)

		// Use the new affinity path for stream batches too
		if err := backend.processAffinityBatch(batch); err != nil {
			return err
		}
	}
	return nil
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
func (backend *Backend) UniversalBitwise(
	a, b, dst unsafe.Pointer, numValues uint32,
) error {
	as := unsafe.Slice((*[primitive.Words]uint64)(a), numValues)

	for v := uint32(0); v < numValues; v++ {
		aValue := (*primitive.Value)(unsafe.Pointer(&as[v]))
		backend.executeProgram(aValue, b, dst, v)
	}

	return nil
}

// executeProgram runs truth-table instructions from the Value's boot + program region.
//
// Bit 31:       src immediate (value is the 7-bit index itself, not a memory read)
// Halt:         full 32-bit instruction == 0
// Skip-if-zero: zero result skips the next instruction
// PC register:  writable for jumps
// Range deref:  *r0 or *r3 use register triples (context, offset, length)
func (backend *Backend) executeProgram(aValue *primitive.Value, b, dst unsafe.Pointer, v uint32) {
	bs := unsafe.Slice((*[primitive.Words]uint64)(b), v+1)
	ds := unsafe.Slice((*[primitive.Words]uint64)(dst), v+1)

	aWords := (*[primitive.Words]uint64)(unsafe.Pointer(aValue))
	copy(ds[v][:], aWords[:])

	ds[v][core.Cfg.RegPC] = 0
	skip := false

	for {
		pc := int(ds[v][core.Cfg.RegPC])
		if pc < 0 || pc >= core.Cfg.MaxPC {
			break
		}

		instr := aValue.ReadVMInstruction(pc)
		if instr == 0 {
			break
		}

		ds[v][core.Cfg.RegPC] = uint64(pc + 1)

		if skip {
			skip = false
			continue
		}

		s1Imm := (instr & (1 << 31)) != 0

		parseOp := func(shift uint) (bool, bool, uint32) {
			block := (instr >> shift) & 0x1FF
			return (block & (1 << 8)) != 0, (block & (1 << 7)) != 0, block & 0x7F
		}

		s1Deref, s1Part, s1Idx := parseOp(4)
		_, s2Part, s2Idx := parseOp(13)
		dDeref, dPart, dIdx := parseOp(22)

		opBits := uint64(instr & 0xF)
		m0 := uint64(0) - (opBits & 1)
		m1 := uint64(0) - ((opBits >> 1) & 1)
		m2 := uint64(0) - ((opBits >> 2) & 1)
		m3 := uint64(0) - ((opBits >> 3) & 1)
		k1 := m0 ^ m2
		k2 := m0 ^ m1
		k3 := m0 ^ m1 ^ m2 ^ m3

		selfWords := (*[primitive.Words]uint64)(unsafe.Pointer(&ds[v]))
		partWords := (*[primitive.Words]uint64)(unsafe.Pointer(&bs[v]))

		pickSrc := func(part bool) *[primitive.Words]uint64 {
			if part {
				return partWords
			}
			return selfWords
		}

		// Check if register index starts a triple (r0→Reg0, r3→Reg3)
		isTripleStart := func(idx uint32) bool {
			return idx == uint32(core.Cfg.R0) || idx == uint32(core.Cfg.R3)
		}

		// Range deref: *r0 uses (r0=context, r1=offset, r2=length)
		//              *r3 uses (r3=context, r4=offset, r5=length)
		if (s1Deref || dDeref) && (isTripleStart(s1Idx) || isTripleStart(dIdx)) {
			// Resolve source range
			var srcArr *[primitive.Words]uint64
			var srcOff, srcLen uint32

			if s1Imm {
				// Immediate src in range mode: broadcast the value
				srcArr = selfWords
				srcOff = 0
				srcLen = 1
			} else if s1Deref && isTripleStart(s1Idx) {
				ctx := selfWords[s1Idx]
				srcOff = uint32(selfWords[s1Idx+1])
				srcLen = uint32(selfWords[s1Idx+2])
				if ctx == 1 {
					srcArr = partWords
				} else {
					srcArr = selfWords
				}
			} else {
				src := pickSrc(s1Part)
				if s1Idx < primitive.Words {
					srcOff = s1Idx
					srcLen = 1
					srcArr = src
				}
			}

			// Resolve dest range
			var dstArr *[primitive.Words]uint64
			var dstOff, dstLen uint32

			if dDeref && isTripleStart(dIdx) {
				ctx := selfWords[dIdx]
				dstOff = uint32(selfWords[dIdx+1])
				dstLen = uint32(selfWords[dIdx+2])
				if ctx == 1 {
					dstArr = partWords
				} else {
					dstArr = selfWords
				}
			} else {
				dstArr = pickSrc(dPart)
				dstOff = dIdx
				dstLen = 1
			}

			opLen := srcLen
			if dstLen < opLen {
				opLen = dstLen
			}

			allZero := true
			for i := uint32(0); i < opLen; i++ {
				sp := srcOff + i
				dp := dstOff + i
				if sp >= primitive.Words || dp >= primitive.Words {
					continue
				}

				var left uint64
				if s1Imm {
					left = uint64(s1Idx)
				} else {
					left = srcArr[sp]
				}
				right := dstArr[dp]

				result := m0 ^ (k1 & left) ^ (k2 & right) ^ (k3 & (left & right))
				dstArr[dp] = result
				if result != 0 {
					allZero = false
				}
			}

			if allZero {
				skip = true
			}
			continue
		}

		// Single-word path
		var left uint64
		if s1Imm {
			left = uint64(s1Idx)
		} else {
			src := pickSrc(s1Part)
			idx := s1Idx
			if s1Deref && idx < primitive.Words {
				idx = uint32(src[idx])
			}
			if idx >= primitive.Words {
				continue
			}
			left = src[idx]
		}

		destSrc := pickSrc(s2Part)
		destIdx := s2Idx
		if dDeref && dIdx < primitive.Words {
			destIdx = uint32(selfWords[dIdx])
		}
		if destIdx >= primitive.Words {
			continue
		}

		right := destSrc[destIdx]
		result := m0 ^ (k1 & left) ^ (k2 & right) ^ (k3 & (left & right))

		writeSrc := pickSrc(dPart)
		writeSrc[destIdx] = result

		if result == 0 {
			skip = true
		}
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
