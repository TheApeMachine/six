package primitive

import (
	"io"
	"unsafe"
)

/*
Memory Layout of the 8192-bit Value

The Value is a flat array of 128 uint64 words = 8192 bits total.
These bits are partitioned into regions with distinct roles:

  REGION 0 — DATA FIELD (words 0–59)
	Stores 57 packed TokenIDs followed by:
	- ValueID (word 57)
	- PrevValueID (word 58)
	- NextValueID (word 59)
	TokenIDs use the byte value and the sequence index:
	(byte_value << 32) | sequence_index

  REGION 1 — INSTRUCTION REGISTER (bits 3840–3843, 4 bits)
    Encodes one of 16 truth-table operations as a 4-bit index.

  REGION 2, 3, 4 — UNUSED REGISTERS
    Intentionally uncommitted for now.

  REGION 5 — INSTRUCTION FLAG (bit 8191)
    Single bit. When set, this Value acts as an in-band control frame.
*/

const (
	Words    = 128
	ByteSize = Words * 8
	CoreBits = 8191

	Region0TokenCount       = 57
	Region0ValueIDIndex     = Region0TokenCount
	Region0PrevValueIDIndex = Region0ValueIDIndex + 1
	Region0NextValueIDIndex = Region0PrevValueIDIndex + 1
	DataWords               = Region0NextValueIDIndex + 1
	DataBits                = DataWords * 64

	StateSlotIndex   = 60
	StateSeqIndex    = 61
	StateAccumulator = 62

	InstrStart = StateAccumulator + 1
	InstrBits  = 4

	InstructionMask uint64 = 1 << 63

	// SignalMask is the mechanical bit-pattern used to reset the sequence index.
	// 0xF (1111) means the sequence resets whenever the bottom 4 bits of the XOR signal hit 0.
	SignalMask = 0xF
)

type Region int

const (
	RegionData        = Region(0)
	RegionInstruction = Region(InstrStart)
)

/*
Value is the core substrate. It represents Data, Behavior, and the physical pipeline.
Values implement the io.ReadWriteCloser interface so they can be piped endlessly
through the Orchestrator loop.
*/
type Value [Words]uint64

var (
	valueTo   func(*Value, []byte)
	valueFrom func([]byte, *Value)
)

func init() {
	x := uint16(1)

	// Fast-path memory alignment for little-endian architectures
	if *(*byte)(unsafe.Pointer(&x)) == 1 {
		valueTo = func(v *Value, p []byte) {
			copy(p, unsafe.Slice((*byte)(unsafe.Pointer(&v[0])), ByteSize))
		}
		valueFrom = func(p []byte, v *Value) {
			copy(unsafe.Slice((*byte)(unsafe.Pointer(&v[0])), ByteSize), p)
		}
		return
	}

	valueTo = valueToPortable
	valueFrom = valueFromPortable
}

func valueToPortable(v *Value, p []byte) {
	for i := 0; i < Words; i++ {
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
	for i := 0; i < Words; i++ {
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

func NewValue() *Value {
	return &Value{}
}

/*
Read implements io.Reader. It serializes the Value's physical 1024-byte frame
so it can be piped into the Backend or the feedback loop.
*/
func (value *Value) Read(p []byte) (int, error) {
	valueTo(value, p)
	return ByteSize, io.EOF
}

/*
Write implements io.Writer. It streams raw bytes into Region 0.
It enforces the physical rules of "Collision is Compression", path extension,
and organic sequence resets driven by a bitwise XOR signal accumulator.
*/
func (value *Value) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	bytesConsumed := 0

	for _, b := range p {
		// Read the current physical state from Region 2
		slot := value[StateSlotIndex]
		seqIndex := value[StateSeqIndex]
		accumulator := value[StateAccumulator]

		// Stop if Region 0 is physically full.
		if slot >= Region0TokenCount {
			break
		}

		token := Tokenize(b, seqIndex)
		existing := value[slot]

		switch existing {
		case token:
			// 1. COLLISION IS COMPRESSION
			// We are traversing an existing path. We do not write to memory,
			// but we consume the byte and update our XOR signal.
			bytesConsumed++
			accumulator ^= token

		case 0:
			// 2. PATH EXTENSION
			// The physical slot is empty. We extend the current path.
			value[slot] = token
			bytesConsumed++
			accumulator ^= token

		default:
			// 3. DIVERGENCE
			// The memory holds a different path. We break immediately.
			// The pipeline will take the unconsumed bytes and feed them
			// into a new Value.
			return len(p), nil
		}

		// 4. THE SIGNAL (Organic Boundaries)
		// We use the mechanical bit-pattern of the accumulator to find natural resets.
		if accumulator&SignalMask == 0 {
			value[StateSeqIndex] = 0
		} else {
			value[StateSeqIndex] = seqIndex + 1
		}

		// 5. UPDATE PHYSICAL STATE
		// Move to the next physical slot and save the accumulator
		value[StateSlotIndex] = slot + 1
		value[StateAccumulator] = accumulator
	}

	return len(p), nil
}

/*
Close satisfies io.Closer for pipeline composition.
*/
func (value *Value) Close() error {
	return nil
}

/*
Tokenize securely packs a byte and its sequence index into a 64-bit hardware token.
*/
func Tokenize(b byte, index uint64) uint64 {
	return (uint64(b) << 32) | index
}

func region0TokenIndexOK(index int) bool {
	return index >= 0 && index < Region0TokenCount
}

func (value *Value) TokenID(index int) uint64 {
	if !region0TokenIndexOK(index) {
		return 0
	}
	return value[index]
}

func (value *Value) SetTokenID(index int, token uint64) bool {
	if !region0TokenIndexOK(index) {
		return false
	}
	value[index] = token
	return true
}

func (value *Value) TokenIDs() []uint64 {
	tokens := make([]uint64, Region0TokenCount)
	copy(tokens, value[:Region0TokenCount])
	return tokens
}

func (value *Value) SetTokenIDs(tokens []uint64) int {
	n := min(len(tokens), Region0TokenCount)
	copy(value[:n], tokens[:n])
	return n
}

func (value *Value) Region0Span(start, length int) []uint64 {
	if start < 0 || start >= Region0TokenCount || length <= 0 {
		return nil
	}
	end := min(start+length, Region0TokenCount)
	span := make([]uint64, end-start)
	copy(span, value[start:end])
	return span
}

func (value *Value) ValueID() uint64 {
	return value[Region0ValueIDIndex]
}

func (value *Value) SetValueID(id uint64) {
	value[Region0ValueIDIndex] = id
}

func (value *Value) PrevValueID() uint64 {
	return value[Region0PrevValueIDIndex]
}

func (value *Value) SetPrevValueID(id uint64) {
	value[Region0PrevValueIDIndex] = id
}

func (value *Value) NextValueID() uint64 {
	return value[Region0NextValueIDIndex]
}

func (value *Value) SetNextValueID(id uint64) {
	value[Region0NextValueIDIndex] = id
}

/*
BytesToValue overlays a 1024-byte slice onto the Value pointer via unsafe memory access.
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
ValueToBytes writes the Value's 1024-byte frame into p.
*/
func ValueToBytes(v *Value, p []byte) error {
	valueTo(v, p)
	return nil
}
