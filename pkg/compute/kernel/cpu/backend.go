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

type Region struct {
	Start int
	Bits  int
}

var (
	RegionData        = Region{Start: 0, Bits: primitive.DataBits}
	RegionInstruction = Region{Start: primitive.InstrStart, Bits: primitive.InstrBits}
	RegionAffinity    = Region{Start: primitive.RegionAffinityStart, Bits: primitive.RegionAffinityBits}
	RegionProgram     = Region{Start: primitive.RegionProgramStart, Bits: primitive.RegionProgramBits}
	RegionLink        = Region{Start: primitive.RegionLinkStart, Bits: primitive.RegionLinkBits}
)

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
		emitted[primitive.StateSlotIndex] = 1

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

func setInstructionFlag(value *primitive.Value) {
	value[primitive.Words-1] |= primitive.InstructionMask
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
UniversalBitwise is the pure hardware ALU. It supports both single-tick mode
(using RegionInstruction) and full 64-tick program mode (using RegionProgram).
This implements the in-band executable substrate.
*/
func (backend *Backend) UniversalBitwise(
	a, b, dst unsafe.Pointer, numValues uint32,
) error {
	as := unsafe.Slice((*[primitive.Words]uint64)(a), numValues)
	bs := unsafe.Slice((*[primitive.Words]uint64)(b), numValues)
	ds := unsafe.Slice((*[primitive.Words]uint64)(dst), numValues)

	for v := uint32(0); v < numValues; v++ {
		// Only cast the pointers we actually need for ReadRegion/WriteRegion
		aValue := (*primitive.Value)(unsafe.Pointer(&as[v]))
		dstValue := (*primitive.Value)(unsafe.Pointer(&ds[v]))

		// O(1) check: inspect the 4 program-region words directly.
		if aValue.HasProgram() {
			// NEW: Multi-tick program execution mode
			backend.executeProgram(aValue, b, dst, v)
			continue
		}

		// 1. IN-BAND DECODE: Read the 4-bit operation directly from Value A (legacy single-tick mode)
		opBits := ReadRegion(aValue, RegionInstruction) & 0xF

		// 2. HARDWARE LOGIC: Derive the universal boolean gates
		m0 := uint64(0) - (opBits & 1)
		m1 := uint64(0) - ((opBits >> 1) & 1)
		m2 := uint64(0) - ((opBits >> 2) & 1)
		m3 := uint64(0) - ((opBits >> 3) & 1)
		k1 := m0 ^ m2
		k2 := m0 ^ m1
		k3 := m0 ^ m1 ^ m2 ^ m3

		// 3. EXECUTE: Run the truth table across the physical Data slots
		for i := 0; i < primitive.Region0TokenCount; i++ {
			left := as[v][i]
			right := bs[v][i]
			ds[v][i] = m0 ^ (k1 & left) ^ (k2 & right) ^ (k3 & (left & right))
		}

		// 4. PERSIST STATE: Ensure the destination Value retains the instruction state
		WriteRegion(dstValue, RegionInstruction, opBits)
		if (aValue[primitive.Words-1] & primitive.InstructionMask) != 0 {
			setInstructionFlag(dstValue)
		}
	}

	return nil
}

// executeProgram runs a full 64-tick program from Region 3 of the controlling Value.
// This implements the "self-executing packet" concept where each Value
// carries its own microcode.
//
// v is the index of the value pair within the b/dst buffers; the function reads
// b[v] and writes dst[v] so that multi-value batches are handled correctly.
// aValue is never mutated: its Region 0 data is copied into a local workWords
// array that is evolved each tick, keeping the caller's input intact.
func (backend *Backend) executeProgram(aValue *primitive.Value, b, dst unsafe.Pointer, v uint32) {
	// Slice the shared buffers to cover at least v+1 elements.
	bs := unsafe.Slice((*[primitive.Words]uint64)(b), v+1)
	ds := unsafe.Slice((*[primitive.Words]uint64)(dst), v+1)

	// Copy Region 0 of aValue into a local working buffer so we never
	// mutate the caller's input value across ticks.
	var workWords [primitive.Words]uint64
	aWords := (*[primitive.Words]uint64)(unsafe.Pointer(aValue))
	copy(workWords[:], aWords[:])

	// For each tick in the 64-op program
	for pc := 0; pc < 64; pc++ {
		opBits := aValue.ProgramOp(pc)
		if opBits == 0 {
			break // HALT/NOP
		}

		// Derive the universal boolean gates for this opcode
		op := uint64(opBits)
		m0 := uint64(0) - (op & 1)
		m1 := uint64(0) - ((op >> 1) & 1)
		m2 := uint64(0) - ((op >> 2) & 1)
		m3 := uint64(0) - ((op >> 3) & 1)
		k1 := m0 ^ m2
		k2 := m0 ^ m1
		k3 := m0 ^ m1 ^ m2 ^ m3

		// Execute across Region 0 data using the v-th input pair.
		for i := 0; i < primitive.Region0TokenCount; i++ {
			left := workWords[i]
			right := bs[v][i]
			workWords[i] = m0 ^ (k1 & left) ^ (k2 & right) ^ (k3 & (left & right))
		}
	}

	// Write the final evolved Region 0 into dst[v] and clear instruction bits.
	copy(ds[v][:primitive.Region0TokenCount], workWords[:primitive.Region0TokenCount])
	dstValue := (*primitive.Value)(unsafe.Pointer(&ds[v]))
	// Clear instruction bits for program mode - the program itself defines behavior.
	WriteRegion(dstValue, RegionInstruction, 0)
}

func WriteBits(value *primitive.Value, startBit, bitLen int, payload uint64) {
	word := startBit >> 6
	shift := uint(startBit & 63)

	if bitLen <= 0 || bitLen > 64 {
		return
	}

	mask := uint64(0xFFFFFFFFFFFFFFFF)
	if bitLen < 64 {
		mask = (uint64(1) << uint(bitLen)) - 1
	}
	payload &= mask

	value[word] &^= mask << shift
	value[word] |= payload << shift

	if shift+uint(bitLen) > 64 {
		hiBits := int(shift) + bitLen - 64
		hiMask := (uint64(1) << uint(hiBits)) - 1
		value[word+1] &^= hiMask
		value[word+1] |= payload >> (64 - shift)
	}
}

func ReadBits(value *primitive.Value, startBit, bitLen int) uint64 {
	word := startBit >> 6
	shift := uint(startBit & 63)

	if bitLen <= 0 || bitLen > 64 {
		return 0
	}

	mask := uint64(0xFFFFFFFFFFFFFFFF)
	if bitLen < 64 {
		mask = (uint64(1) << uint(bitLen)) - 1
	}

	v := value[word] >> shift
	if shift+uint(bitLen) > 64 {
		v |= value[word+1] << (64 - shift)
	}

	return v & mask
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

func ReadRegion(
	value *primitive.Value, region Region,
) uint64 {
	if region.Bits > 64 {
		return 0
	}

	return ReadBits(value, region.Start, region.Bits)
}

func WriteRegion(
	value *primitive.Value, region Region, payload uint64,
) {
	if region.Bits > 64 {
		return
	}

	WriteBits(value, region.Start, region.Bits, payload)
}
