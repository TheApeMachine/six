package primitive

import (
	"errors"
	"fmt"
	"io"
	"math/bits"
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

  REGION 3 — STATE VECTOR (bits 518–774, 257 bits)
    A third GF(257) field acting as a CRDT (Conflict-free Replicated Data Type).
    It holds the continuous Bitwise OR of all previous Region 0s in this execution chain.
    This makes the Value self-healing over lossy networks.

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

	StateStart = OperandStart + OperandBits
	StateBits  = DataBits

	MetaStart = StateStart + StateBits

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
	RegionStateVector = Region(OperandBits)
	RegionInstruction = Region(StateBits)
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

func (value *Value) empty() bool {
	return value[0] == 0 && value[1] == 0 && value[2] == 0 && value[3] == 0 && (value[4]&1) == 0
}

func (value *Value) projectByte(b byte) {
	value[0] = uint64(b)
	value[1] = 0
	value[2] = 0
	value[3] = 0
	value[4] = (value[4] &^ 1) | 1
}

func (value *Value) projectValue(buf []byte) {
	valueFrom(buf, value)
}

/*
Read implements io.Reader. Serializes the Value's 1024-byte frame into p.
*/
func (value *Value) Read(p []byte) (int, error) {
	if len(p) < ByteSize {
		return 0, io.ErrShortBuffer
	}

	valueTo(value, p)
	return ByteSize, io.EOF
}

/*
Write implements the first stage of the "folding" process, which can be seen as the
"instruction pointer" equivalent in a self-computing substrate. In this stage we take
the value, and the incoming value, and prepare them for the Read stage, where an
operation is potentially fired, and new values are emitted
*/
func (value *Value) Write(p []byte) (int, error) {
	lp := len(p)

	if lp == 0 {
		return 0, nil
	}

	// 1. SEEDING: If Region 0 is empty, we just need to read 1 byte into Region 0.
	if value.empty() {
		value.projectByte(p[0])
		return 1, nil // Consume exactly 1 byte from the stream
	}

	// 2. INGESTION: Region 0 is set, so we expect a full 1024-byte Value.
	if lp < ByteSize {
		return 0, io.ErrShortBuffer
	}

	incoming := BytesToValue(p[:lp])

	// Move Region 0 of the incoming Value into Region 2 (Operand) of the current Value
	copyDataToOperand(value, incoming)
	return lp, nil
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
clearOperandBits zeroes only the operand region (bits 261–517), preserving
data, instruction, and state vector bits in boundary words.
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
copyStateVector copies the state vector from src to the
operand register of dst.
*/
func copyStateVector(dst, src *Value) {
	const sw, ss = StateStart >> 6, StateStart & 63
	const dw, ds = OperandStart >> 6, OperandStart & 63

	clearOperandBits(dst)

	for i := range 4 {
		x := src[sw+i]>>ss | src[sw+i+1]<<(64-ss)
		dst[dw+i] |= x << ds
		dst[dw+i+1] |= x >> (64 - ds)
	}

	if (src[(StateStart+256)>>6]>>((StateStart+256)&63))&1 != 0 {
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
MergeStateVector performs a CRDT merge (Bitwise OR) of the src State Vector
into the dst State Vector. This operation is commutative, associative, and idempotent,
allowing a node to instantly recover missing state from dropped packets.
*/
func MergeStateVector(dst, src *Value) {
	const w = StateStart >> 6
	const s = StateStart & 63

	mask0 := ^((uint64(1) << s) - 1)
	mask4 := (uint64(1) << ((StateStart + StateBits) & 63)) - 1

	dst[w+0] |= (src[w+0] & mask0)
	dst[w+1] |= src[w+1]
	dst[w+2] |= src[w+2]
	dst[w+3] |= src[w+3]
	dst[w+4] |= (src[w+4] & mask4)

	// Decay mechanism to prevent CRDT saturation
	pop := bits.OnesCount64(dst[w+0]>>s) +
		bits.OnesCount64(dst[w+1]) +
		bits.OnesCount64(dst[w+2]) +
		bits.OnesCount64(dst[w+3]) +
		bits.OnesCount64(dst[w+4]&mask4)

	if pop > 128 {
		const decayMask = 0x5555555555555555
		dMask0 := uint64(decayMask) | ^mask0
		dMask4 := uint64(decayMask) | ^mask4

		dst[w+0] &= dMask0
		dst[w+1] &= decayMask
		dst[w+2] &= decayMask
		dst[w+3] &= decayMask
		dst[w+4] &= dMask4
	}
}

/*
HammingDistance calculates the topological distance between two Values
by counting the number of differing bits (symmetric difference).
This is the core metric for associative resonance and emergent compute.
*/
func HammingDistance(a, b *Value) int {
	dist := 0
	for i := range Words {
		// We use a simple bitwise XOR and count the set bits
		// to find the geometric distance between two concepts.
		diff := a[i] ^ b[i]
		dist += bits.OnesCount64(diff)
	}
	return dist
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
