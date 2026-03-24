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
	RegionOperand     = Region{Start: primitive.OperandStart, Bits: primitive.OperandBits}
	RegionStateVector = Region{Start: primitive.StateStart, Bits: primitive.StateBits}
	RegionMeta        = Region{Start: primitive.MetaStart, Bits: primitive.CoreBits - primitive.MetaStart}
)

/*
Backend is the CPU kernel backend. It mirrors
the Metal/CUDA dispatch surface so that every
GPU kernel has a verified CPU fallback.
*/
type Backend struct {
	pr *ringbuffer.PipeReader
	pw *ringbuffer.PipeWriter
	rb *ringbuffer.RingBuffer
}

/*
NewBackend returns a CPU Backend.
*/
func NewBackend() *Backend {
	rb := ringbuffer.New(1024)
	pr, pw := rb.Pipe()

	return &Backend{
		pr: pr,
		pw: pw,
		rb: rb,
	}
}

/*
Available returns the number of logical CPU cores.
*/
func Available() int {
	return runtime.NumCPU()
}

func (backend *Backend) Read(p []byte) (n int, err error) {
	if backend.rb.Length() == 0 {
		return 0, io.EOF
	}

	n, err = backend.pr.Read(p)

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

	incoming := primitive.BytesToValue(p)
	// Fast path aliases p[:ByteSize]; copy fallback uses a separate Value buffer.
	frameInPlace := len(p) >= primitive.ByteSize &&
		uintptr(unsafe.Pointer(incoming)) == uintptr(unsafe.Pointer(&p[0]))

	if len(p) >= primitive.ByteSize {
		// Spin impulse: motor rotates topology; data shifts across instruction/operand slots.
		if err := backend.Accumulate(
			unsafe.Pointer(incoming),
			unsafe.Pointer(incoming),
			1,
		); errnie.Error(err) != nil {
			return 0, err
		}

		// Spark impulse: structural pressure → ALU (boolean op on Data × Operand → Data).
		if incoming[primitive.OperandStart>>6] != 0 {
			instr := uint8(ReadRegion(incoming, RegionInstruction) & 0xF)
			if err := backend.UniversalBitwise(
				instr,
				unsafe.Pointer(incoming),
				unsafe.Pointer(incoming),
				unsafe.Pointer(incoming),
				1,
			); errnie.Error(err) != nil {
				return 0, err
			}

			// CRDT Fold: Merge new Data into State Vector.
			if err := backend.UpdateStateVector(
				unsafe.Pointer(incoming),
				1,
			); errnie.Error(err) != nil {
				return 0, err
			}

			// Clear the operand so it doesn't re-fire until new data arrives.
			if err := backend.ClearOperand(
				unsafe.Pointer(incoming),
				1,
			); errnie.Error(err) != nil {
				return 0, err
			}
		}

		if !frameInPlace {
			if err := primitive.ValueToBytes(incoming, p); errnie.Error(err) != nil {
				return 0, err
			}
		}
	}

	if n, err = backend.pw.Write(p); errnie.Error(err) != nil {
		return
	}

	return n, nil
}

func (backend *Backend) Close() error {
	return nil
}

/*
Accumulate interleaves motor application with bitwise composition
for N Value pairs. It applies the incoming Value's derived motor
to the current state, then composes them via OR (LCM), natively
mapping sequence chronology to discrete topology.
*/
func (backend *Backend) Accumulate(
	incoming, state unsafe.Pointer, numValues uint32,
) error {
	inSlices := unsafe.Slice(
		(*[primitive.Words]uint64)(incoming), numValues,
	)

	stateSlices := unsafe.Slice(
		(*[primitive.Words]uint64)(state), numValues,
	)

	for v := uint32(0); v < numValues; v++ {
		s, t := deriveMotor(&inSlices[v])

		var mapped [primitive.Words]uint64
		applyMotor(&stateSlices[v], &mapped, s, t)

		for i := uint32(0); i < primitive.Words; i += 8 {
			stateSlices[v][i+0] = mapped[i+0] | inSlices[v][i+0]
			stateSlices[v][i+1] = mapped[i+1] | inSlices[v][i+1]
			stateSlices[v][i+2] = mapped[i+2] | inSlices[v][i+2]
			stateSlices[v][i+3] = mapped[i+3] | inSlices[v][i+3]
			stateSlices[v][i+4] = mapped[i+4] | inSlices[v][i+4]
			stateSlices[v][i+5] = mapped[i+5] | inSlices[v][i+5]
			stateSlices[v][i+6] = mapped[i+6] | inSlices[v][i+6]
			stateSlices[v][i+7] = mapped[i+7] | inSlices[v][i+7]
		}

		stateSlices[v][primitive.Words-1] &= primitive.LastMask
	}

	return nil
}

/*
UniversalBitwise executes ANY of the 16 possible boolean operations
based purely on the 4-bit instruction truth table. No branching required.
It operates specifically on the defined regions:
op(A.Region0, B.Region2) -> dst.Region3
*/
func (backend *Backend) UniversalBitwise(
	instrBits uint8, a, b, dst unsafe.Pointer, numValues uint32,
) error {
	as := unsafe.Slice((*[primitive.Words]uint64)(a), numValues)
	bs := unsafe.Slice((*[primitive.Words]uint64)(b), numValues)
	ds := unsafe.Slice((*[primitive.Words]uint64)(dst), numValues)

	// Expand 4 instruction bits into full-word masks.
	m0 := uint64(0) - uint64(instrBits&1)
	m1 := uint64(0) - uint64((instrBits>>1)&1)
	m2 := uint64(0) - uint64((instrBits>>2)&1)
	m3 := uint64(0) - uint64((instrBits>>3)&1)

	// Algebraic normal form:
	// f = m0 ^ ((m0^m2)&A) ^ ((m0^m1)&B) ^ ((m0^m1^m2^m3)&(A&B))
	// This is equivalent to SOP truth-table form with fewer ops.
	k1 := m0 ^ m2
	k2 := m0 ^ m1
	k3 := m0 ^ m1 ^ m2 ^ m3

	const (
		// Region 2 (operand) starts at word 4 bit 5.
		b0w = primitive.OperandStart >> 6
		// Region 0 (data) starts at word 0 bit 0.
		d0w = 0
		d0s = 0

		preserveDstWord4 = ^uint64(1) // Keep bits above data field end.
	)

	for v := range numValues {
		a0 := as[v][0]
		a1 := as[v][1]
		a2 := as[v][2]
		a3 := as[v][3]
		a4 := as[v][4] & 1

		bw4 := bs[v][b0w+0]
		bw5 := bs[v][b0w+1]
		bw6 := bs[v][b0w+2]
		bw7 := bs[v][b0w+3]
		bw8 := bs[v][b0w+4]

		// Extract Region 2 (257 bits) into 5 logical lanes.
		b0 := (bw4 >> 5) | (bw5 << 59)
		b1 := (bw5 >> 5) | (bw6 << 59)
		b2 := (bw6 >> 5) | (bw7 << 59)
		b3 := (bw7 >> 5) | (bw8 << 59)
		b4 := (bw8 >> 5) & 1

		r0 := m0 ^ (k1 & a0) ^ (k2 & b0) ^ (k3 & (a0 & b0))
		r1 := m0 ^ (k1 & a1) ^ (k2 & b1) ^ (k3 & (a1 & b1))
		r2 := m0 ^ (k1 & a2) ^ (k2 & b2) ^ (k3 & (a2 & b2))
		r3 := m0 ^ (k1 & a3) ^ (k2 & b3) ^ (k3 & (a3 & b3))
		r4 := (m0 ^ (k1 & a4) ^ (k2 & b4) ^ (k3 & (a4 & b4))) & 1

		// Write 257 result bits into Region 0, preserving bits
		// outside the data window in boundary words.
		ds[v][d0w+0] = r0
		ds[v][d0w+1] = r1
		ds[v][d0w+2] = r2
		ds[v][d0w+3] = r3
		ds[v][d0w+4] = (ds[v][d0w+4] & preserveDstWord4) | r4
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

/*
UpdateStateVector merges the current Data Field (Region 0) into the State Vector (Region 3)
using a bitwise OR, effectively creating a CRDT lattice of all computed states.
*/
func (backend *Backend) UpdateStateVector(state unsafe.Pointer, numValues uint32) error {
	stateSlices := unsafe.Slice((*[primitive.Words]uint64)(state), numValues)

	const (
		s0w = primitive.StateStart >> 6
		s0s = primitive.StateStart & 63
	)

	for v := uint32(0); v < numValues; v++ {
		r0 := stateSlices[v][0]
		r1 := stateSlices[v][1]
		r2 := stateSlices[v][2]
		r3 := stateSlices[v][3]
		r4 := stateSlices[v][4] & 1

		// OR into State Vector (word 8 bit 6)
		stateSlices[v][s0w+0] |= (r0 << s0s)
		stateSlices[v][s0w+1] |= (r0 >> (64 - s0s)) | (r1 << s0s)
		stateSlices[v][s0w+2] |= (r1 >> (64 - s0s)) | (r2 << s0s)
		stateSlices[v][s0w+3] |= (r2 >> (64 - s0s)) | (r3 << s0s)
		stateSlices[v][s0w+4] |= (r3 >> (64 - s0s)) | (r4 << s0s)
	}
	return nil
}

/*
ClearOperand zeroes out the Operand Register (Region 2) after an operation has fired.
*/
func (backend *Backend) ClearOperand(state unsafe.Pointer, numValues uint32) error {
	stateSlices := unsafe.Slice((*[primitive.Words]uint64)(state), numValues)

	const (
		lo       = primitive.OperandStart
		hi       = primitive.OperandStart + primitive.OperandBits - 1
		loW, loS = lo >> 6, lo & 63
		hiW, hiS = hi >> 6, hi & 63
	)

	for v := uint32(0); v < numValues; v++ {
		if loW == hiW {
			mask := ((uint64(1) << (hiS - loS + 1)) - 1) << uint(loS)
			stateSlices[v][loW] &^= mask
			continue
		}

		stateSlices[v][loW] &^= ^((uint64(1) << uint(loS)) - 1)
		for w := loW + 1; w < hiW; w++ {
			stateSlices[v][w] = 0
		}
		stateSlices[v][hiW] &^= (uint64(1) << uint(hiS+1)) - 1
	}
	return nil
}
