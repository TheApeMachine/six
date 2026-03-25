package cpu

import (
	"io"
	"math/bits"
	"runtime"
	"unsafe"

	"github.com/smallnest/ringbuffer"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

const InstructionByteMask byte = 0x80 // 10000000 in binary

type Region struct {
	Start int
	Bits  int
}

var (
	RegionData        = Region{Start: 0, Bits: primitive.DataBits}
	RegionInstruction = Region{Start: primitive.InstrStart, Bits: primitive.InstrBits}
)

/*
Backend is the CPU kernel backend. It mirrors
the Metal/CUDA dispatch surface so that every
GPU kernel has a verified CPU fallback.
*/
type Backend struct {
	pr        *ringbuffer.PipeReader
	pw        *ringbuffer.PipeWriter
	rb        *ringbuffer.RingBuffer
	outPr     *ringbuffer.PipeReader
	outPw     *ringbuffer.PipeWriter
	outRb     *ringbuffer.RingBuffer
	batchCap  int
	residents []primitive.Value
	nextID    uint64
}

type backendOption func(*Backend)

/*
NewBackend returns a CPU Backend.
*/
func NewBackend(opts ...backendOption) *Backend {
	backend := &Backend{
		batchCap: max(2, runtime.NumCPU()-1),
		nextID:   1,
	}

	for _, opt := range opts {
		opt(backend)
	}

	if backend.batchCap < 2 {
		backend.batchCap = 2
	}

	rb := ringbuffer.New(backend.batchCap * primitive.ByteSize)
	pr, pw := rb.Pipe()
	outRb := ringbuffer.New(backend.batchCap * primitive.ByteSize)
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

/*
Available returns the number of logical CPU cores.
*/
func Available() int {
	return runtime.NumCPU()
}

func (backend *Backend) Read(p []byte) (n int, err error) {
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

func (backend *Backend) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	if n, err = backend.pw.Write(p); errnie.Error(err) != nil {
		return 0, err
	}

	if err := backend.processAvailableBatch(); err != nil {
		return 0, err
	}

	return n, nil
}

func (backend *Backend) Close() error {
	return nil
}

func (backend *Backend) processAvailableBatch() error {
	batchBytes := backend.batchCap * primitive.ByteSize
	if batchBytes <= 0 || backend.rb.Length() < batchBytes {
		return nil
	}

	buf := make([]byte, batchBytes)
	if _, err := io.ReadFull(backend.pr, buf); err != nil {
		return err
	}

	// Convert raw bytes back into physical Values
	batch := decodeBatchFrames(buf)
	backend.residents = append(backend.residents, batch...)

	// 1. FIND THE COLLISION
	candidate, ok := strongestCancellation(backend.residents)
	if !ok {
		return nil
	}

	left := &backend.residents[candidate.LeftIndex]
	right := &backend.residents[candidate.RightIndex]

	errnie.Trace(
		"compute.kernel.cpu.backend.processAvailableBatch",
		"candidate", candidate,
		"left", left.TokenIDs(),
		"right", right.TokenIDs(),
	)

	// 2. STRUCTURAL FOLDING
	emitted := buildEmittedValue(left, right, candidate.Span, backend.nextID)

	// 3. CHECK FOR BEHAVIOR
	isInstruction := (left[primitive.Words-1] & primitive.InstructionMask) != 0

	if isInstruction {
		if err := backend.UniversalBitwise(
			unsafe.Pointer(left),
			unsafe.Pointer(right),
			unsafe.Pointer(emitted),
			1,
		); err != nil {
			return err
		}

		opBits := ReadRegion(left, RegionInstruction) & 0xF
		WriteRegion(emitted, RegionInstruction, opBits)
		setInstructionFlag(emitted)
	}

	// 4. LINK AND DUMP TO THE OUTPUT PIPE
	emitted.SetValueID(backend.nextID)
	backend.nextID++

	// Serialize the new Value back into a 1024-byte frame
	frame := make([]byte, primitive.ByteSize)
	if err := primitive.ValueToBytes(emitted, frame); err != nil {
		return err
	}

	// Write the emitted frame back into the pipeline
	_, err := backend.outPw.Write(frame)
	return err
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
UniversalBitwise is the pure hardware ALU. It takes no external opcodes.
It reads the instruction in-band from the Value itself and applies the physical truth table.
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

		// 1. IN-BAND DECODE: Read the 4-bit operation directly from Value A
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
