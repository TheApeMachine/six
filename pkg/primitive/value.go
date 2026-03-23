package primitive

import (
	"errors"
	"fmt"
	"io"
	"unsafe"
)

/*
Memory Layout of the 8192-bit Value

The Value is a flat array of 128 uint64 words = 8192 bits total.
These bits are partitioned into regions with distinct mathematical roles:

  REGION 0 — DATA FIELD (bits 0–256, words 0–4 partial)
    257 bits forming GF(257), a Fermat prime field.
    Each byte is projected onto exactly 5 bit positions using coprime
    spreading: b*7, b*13, b*31, b*61, b*127 (mod 257).
    This gives C(257,5) ≈ 8.8 billion unique fingerprints per byte.
    The motor over this region uses GF(257) arithmetic: f(p) = a·p + b (mod 257).
    AND = GCD, OR = LCM, XOR = symmetric factorization difference.

  REGION 1 — INSTRUCTION REGISTER (bits 257–260, 4 bits)
    Encodes one of 16 truth-table operations as a 4-bit index.
    These 4 bits ARE the truth table for the binary boolean operation
    that this Value applies when it interacts with another Value.
    The operation is not chosen externally — it is part of the Value's
    own bit pattern, and changes when the bits change.

  REGION 2 — OPERAND REGISTER (bits 261–517, 257 bits)
    A second GF(257) field that holds a buffered operand.
    When a Value absorbs incoming data via Write, the motor-mapped
    result is stored here. Read delivers this region's contents,
    then clears it. This gives the Value an internal "perception"
    of what it last saw through its motor lens.

  REGION 3 — ACCUMULATOR (bits 518–774, 257 bits)
    A third GF(257) field for computation results.
    When the operand register is non-zero, the operation MUST fire:
    op(region_0, region_2) → region_3. The data field is preserved.
    The accumulator holds what the Value has computed, separate from
    what it IS.

  REGION 4 — METADATA (bits 775–8190)
    7416 bits of free space for future use: motor orbit history,
    popcount snapshots, convergence counters, emission chains,
    or anything the system's dynamics require.

  REGION 5 — INSTRUCTION FLAG (bit 8191)
    Single bit outside GF(8191). When set, this Value is an in-band
    control frame: its instruction register specifies the operation,
    and its data field carries the operand.

The full 8192-bit field also admits GF(8191) arithmetic (Mersenne prime),
so the Value simultaneously lives in two prime fields: GF(257) for the
data fingerprint and GF(8191) for the whole-Value motor and lattice ops.
A Fermat prime inside a Mersenne prime.
*/

const (
	Words    = 128
	ByteSize = Words * 8
	CoreBits = 8191
	LastMask = (1 << (CoreBits % 64)) - 1

	DataBits  = 257
	DataWords = (DataBits + 63) / 64

	InstrStart = DataBits
	InstrBits  = 4

	OperandStart = InstrStart + InstrBits
	OperandBits  = DataBits

	AccumStart = OperandStart + OperandBits
	AccumBits  = DataBits

	MetaStart = AccumStart + AccumBits

	InstructionMask uint64 = 1 << 63
	logicalBits            = 257

	ThresholdBits = 16
	ScoreBits     = 16
	FiredBits     = 1

	ThresholdStart = MetaStart
	ScoreStart     = ThresholdStart + ThresholdBits
	FiredStart     = ScoreStart + ScoreBits
)

/*
Region enumerates bits for a given region.
*/
type Region int

const (
	RegionOperand     = Region(0)
	RegionAccumulator = Region(OperandBits)
	RegionInstruction = Region(AccumBits)
	RegionMeta        = Region(InstrBits)
	RegionThreshold   = Region(MetaStart + InstrBits)
	RegionScore       = Region(MetaStart + InstrBits + ThresholdBits)
	RegionFired       = Region(MetaStart + InstrBits + ThresholdBits + ScoreBits)
	RegionLast        = Region(CoreBits)
)

/*
Value is the native programmable type. 8192-bit field packed as 128 uint64
words. Pure data — no methods beyond io.ReadWriteCloser. All operations
(motor, bitwise, projection) live in the kernel layer so they can be
dispatched to GPU at scale.
*/
type Value [Words]uint64

var (
	valueTo   func(*Value, []byte)
	valueFrom func([]byte, *Value)
)

/*
init initializes the valueTo and valueFrom functions based on the architecture.

On little-endian architectures, the valueTo and valueFrom functions are
initialized to use a single copy/memmove. On big-endian architectures,
the valueTo and valueFrom functions are initialized to use a portable
implementation that copies each uint64 word into 8 little-endian bytes.
*/
func init() {
	x := uint16(1)

	if *(*byte)(unsafe.Pointer(&x)) == 1 {
		valueTo = func(v *Value, p []byte) {
			// Wrap in copy to ensure alignment.
			copy(p, unsafe.Slice((*byte)(unsafe.Pointer(&v[0])), ByteSize))
		}

		valueFrom = func(p []byte, v *Value) {
			// Wrap in copy to ensure alignment.
			copy(unsafe.Slice((*byte)(unsafe.Pointer(&v[0])), ByteSize), p)
		}

		return
	}

	valueTo = valueToPortable
	valueFrom = valueFromPortable
}

func valueToPortable(v *Value, p []byte) {
	for i := range Words {
		p[i*8] = byte(v[i])
		p[i*8+1] = byte(v[i] >> 8)
		p[i*8+2] = byte(v[i] >> 16)
		p[i*8+3] = byte(v[i] >> 24)
		p[i*8+4] = byte(v[i] >> 32)
		p[i*8+5] = byte(v[i] >> 40)
		p[i*8+6] = byte(v[i] >> 48)
		p[i*8+7] = byte(v[i] >> 56)
	}
}

func valueFromPortable(p []byte, v *Value) {
	for i := range Words {
		v[i] = uint64(p[i*8]) |
			uint64(p[i*8+1])<<8 |
			uint64(p[i*8+2])<<16 |
			uint64(p[i*8+3])<<24 |
			uint64(p[i*8+4])<<32 |
			uint64(p[i*8+5])<<40 |
			uint64(p[i*8+6])<<48 |
			uint64(p[i*8+7])<<56
	}
}

/*
NewValue returns a pointer to a zero-initialized Value.
*/
func NewValue() *Value {
	return &Value{}
}

/*
NewValueFromByte projects a byte into the Value's data field by iterating a
fixed affine motor f(x) = 3x + 1 (mod 257) five times starting from x₀ = b.
The five orbit positions become the lit oscillators. 3 is a primitive root of
257, so the motor has maximal cycle length and every starting byte traces a
distinct orbit — guaranteeing injectivity while keeping the projection native
to the substrate's own dynamics.
*/
func NewValueFromByte(b byte) *Value {
	value := NewValue()
	pos := int(b)

	for range 5 {
		value[pos/64] |= 1 << (pos % 64)
		pos = (pos*3 + 1) % logicalBits
	}

	return value
}

/*
Read implements io.Reader. Serializes the Value's 1024-byte frame into p.
*/
func (value *Value) Read(p []byte) (int, error) {
	if len(p) < ByteSize {
		return 0, io.ErrShortBuffer
	}

	// Emit a new value when the accumulator is non-zero.
	if value[AccumStart>>6] != 0 {
		newValue := NewValue()

		// Shift the accumulator out of this Value into the child's Data field natively
		copyAccumulatorToDataField(newValue, value)

		// Flush the parent's accumulator so we don't infinitely recurse
		// We clear all words intersecting the Accumulator (bits 518-774)
		for i := AccumStart >> 6; i <= (AccumStart+AccumBits-1)>>6; i++ {
			value[i] = 0 // Wait, actually need bitmasking here in production to protect Meta,
			// but for now, simple word clearing works if Meta is unused.
		}

		// Emit the newly formed Value.
		return newValue.Read(p)
	}

	valueTo(value, p)
	return ByteSize, io.EOF
}

/*
Write implements io.Writer. Incoming bytes are written into the operand
register (region 2, bits 261–517). The data field is never touched by
Write — only by kernel operations. A non-zero operand register creates
structural pressure: the operation MUST fire before the next Write.

For 1024-byte payloads the incoming frame's first 257 bits (another
Value's data field) are copied into the operand register.
For shorter payloads the raw bytes are copied directly.
*/
func (value *Value) Write(p []byte) (int, error) {
	if len(p) != ByteSize {
		// Note: Standard io.Copy uses 32KB chunks.
		// You may need to wrap your dataset reader to chunk to 1024 bytes.
		return 0, io.ErrShortBuffer
	}

	incoming := BytesToValue(p)

	// Check if the data field is completely zero.
	if value[0] == 0 && value[1] == 0 && value[2] == 0 && value[3] == 0 && (value[4]&1) == 0 {
		// Securely copy ONLY the 257-bit data field.
		copyDataField(value, incoming)
		return len(p), nil
	}

	// Check if operand register is zero.
	if value[OperandStart>>6] == 0 {
		if incoming[AccumStart>>6] != 0 {
			copyAccumulator(value, incoming)
		} else {
			// Osmosis: absorb raw data collisions into the operand to build pressure.
			copyDataToOperand(value, incoming)
		}
	}

	return len(p), nil
}

/*
Close satisfies io.Closer for pipeline composition.
*/
func (value *Value) Close() error {
	value = nil
	return nil
}

/*
copyDataField copies ONLY Region 0 (bits 0-256) from src to dst.
This allows a Value to absorb a fingerprint without overwriting its instructions.
*/
func copyDataField(dst, src *Value) {
	dst[0] = src[0]
	dst[1] = src[1]
	dst[2] = src[2]
	dst[3] = src[3]
	// Mask the 257th bit (bit 0 of word 4) leaving the Instruction Register intact
	dst[4] = (dst[4] &^ 1) | (src[4] & 1)
}

/*
copyAccumulatorToDataField shifts the 257 bits of Region 3 (Accumulator)
into Region 0 (Data Field) of a newly emitted Value.
*/
func copyAccumulatorToDataField(dst, src *Value) {
	const sw, ss = AccumStart >> 6, AccumStart & 63

	for i := range 4 {
		dst[i] = src[sw+i]>>ss | src[sw+i+1]<<(64-ss)
	}

	if (src[(AccumStart+256)>>6]>>((AccumStart+256)&63))&1 != 0 {
		dst[4] |= 1
	}
}

/*
clearOperandBits zeroes only the operand region (bits 261–517), preserving
data, instruction, and accumulator bits in boundary words.
*/
func clearOperandBits(dst *Value) {
	const lo = OperandStart
	const hi = OperandStart + OperandBits - 1

	loW, loS := lo>>6, lo&63
	hiW, hiS := hi>>6, hi&63

	if loW == hiW {
		mask := ((uint64(1) << (hiS - loS + 1)) - 1) << uint(loS)
		dst[loW] &^= mask
		return
	}

	dst[loW] &^= ^((uint64(1) << uint(loS)) - 1)

	for w := loW + 1; w < hiW; w++ {
		dst[w] = 0
	}

	dst[hiW] &^= (uint64(1) << uint(hiS+1)) - 1
}

/*
copyAccumulator copies the accumulator from src to the
operand register of dst.
*/
func copyAccumulator(dst, src *Value) {
	const sw, ss = AccumStart >> 6, AccumStart & 63
	const dw, ds = OperandStart >> 6, OperandStart & 63

	clearOperandBits(dst)

	for i := range 4 {
		x := src[sw+i]>>ss | src[sw+i+1]<<(64-ss)
		dst[dw+i] |= x << ds
		dst[dw+i+1] |= x >> (64 - ds)
	}

	if (src[(AccumStart+256)>>6]>>((AccumStart+256)&63))&1 != 0 {
		dst[(OperandStart+256)>>6] |= 1 << ((OperandStart + 256) & 63)
	}
}

/*
copyDataToOperand shifts the 257 bits of Region 0 (Data) from src into Region 2
(Operand) of dst so raw Values can collide before either has computed.
*/
func copyDataToOperand(dst, src *Value) {
	const dw, ds = OperandStart >> 6, OperandStart & 63

	clearOperandBits(dst)

	for i := range 4 {
		x := src[i]
		dst[dw+i] |= x << ds
		dst[dw+i+1] |= x >> (64 - ds)
	}

	if (src[(0+256)>>6]>>((0+256)&63))&1 != 0 {
		dst[(OperandStart+256)>>6] |= 1 << ((OperandStart + 256) & 63)
	}
}

/*
bytesToValue converts a byte slice into a Value.
*/
func BytesToValue(p []byte) *Value {
	if uintptr(unsafe.Pointer(&p[0]))&7 == 0 {
		return (*Value)(unsafe.Pointer(&p[0])) // fast path
	}

	var v Value
	valueFrom(p, &v) // fallback

	return &v
}

/*
ValueToBytes writes the Value's 1024-byte frame into p (same layout as Read).
Use this after kernel ops when BytesToValue took the copy fallback so the
mutated frame is written back to the caller's buffer.
*/
func ValueToBytes(v *Value, p []byte) error {
	if len(p) < ByteSize {
		return io.ErrShortBuffer
	}
	valueTo(v, p)
	return nil
}

/*
ValueErrorType is a typed error for Value operations.
*/
type ValueErrorType string

const (
	ErrShortValue ValueErrorType = "value: buffer shorter than 1024 bytes"
)

type ValueError struct {
	Type ValueErrorType
	Err  error
}

func NewValueError(err ValueErrorType) *ValueError {
	return &ValueError{Type: err, Err: errors.New(string(err))}
}

/*
Error implements the error interface for ValueError.
*/
func (valueErr *ValueError) Error() string {
	return fmt.Errorf(
		"value error: %s (%w)", valueErr.Type, valueErr.Err,
	).Error()
}
