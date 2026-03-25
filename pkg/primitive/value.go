package primitive

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"
)

/*
The Value is a flat array of 128 uint64 words = 8192 bits total.
These bits are partitioned into regions with distinct roles:

	DATA REGION (words 0–59)
	Stores 57 packed TokenIDs followed by:
		- ValueID     (word 57)
		- PrevValueID (word 58)
		- NextValueID (word 59)
	TokenIDs use the byte value and the sequence index:
	(byte_value << 32) | sequence_index

  	INSTRUCTION REGISTER (bits 3840–3843, 4 bits)
    Encodes one of 16 truth-table operations as a 4-bit index (single-tick compat).

  	AFFINITY MASK (256 bits)
    Sparse bit-pattern used for fast topological clustering via bitwise AND.

  	PROGRAM REGISTER (256 bits)
    64 × 4-bit instructions. When present, UniversalBitwise runs a full
	64-tick program. 0b0000 = NOP/HALT (early exit).

  	EPHEMERAL LINKS + RESERVED FOR FUTURE USE
    Used to form temporary groupings as participation clusters for programs.
	Remaining bits available for explicit Value grouping or routing tables.

  	INSTRUCTION FLAG (bit 8191)
    Single bit. When set, this Value acts as an in-band control frame.
	Will potentially be used for some other purpose in the future.
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

	StateSlotIndex   = 61
	StateSeqIndex    = 62
	StateAccumulator = 63

	InstrStart = DataBits
	InstrBits  = 4

	RegionAffinityStart = 4096
	RegionAffinityBits  = 256
	RegionProgramStart  = RegionAffinityStart + RegionAffinityBits
	RegionProgramBits   = 256
	RegionLinkStart     = RegionProgramStart + RegionProgramBits
	RegionLinkBits      = 256
	RegionGossipStart   = RegionLinkStart + RegionLinkBits
	RegionGossipBits    = 256
	RegionTTLStart      = RegionGossipStart + RegionGossipBits
	RegionTTLBits       = 8

	InstructionMask uint64 = 1 << 63

	// SignalMask is the mechanical bit-pattern
	// used to reset the sequence index.
	// 0xF (1111) means the sequence resets whenever
	// the bottom 4 bits of the XOR signal hit 0.
	SignalMask = 0xF
)

type Region int

const (
	RegionData        = Region(0)
	RegionInstruction = Region(InstrStart)
	RegionAffinity    = Region(RegionAffinityStart)
	RegionProgram     = Region(RegionProgramStart)
	RegionLink        = Region(RegionLinkStart)
	RegionGossip      = Region(RegionGossipStart)
	RegionTTL         = Region(RegionTTLStart)
)

var (
	ErrChunkBoundary = errors.New("chunk boundary reached")
	// ErrRegion0Full is returned from Write when Region 0 already holds the maximum
	// token count and no input byte can be accepted (loss would occur if ignored).
	ErrRegion0Full = errors.New("region0 full: no bytes written")
	valueTo        func(*Value, []byte)
	valueFrom      func([]byte, *Value)

	globalValueIDCounter uint64 = 1000000 // Start high to avoid clashing with Backend
)

/*
Value is the self-programmable type that acts as the fundamental unit of
computation and memory, while also forming its own substrate. It is an
experimental attempt to design a native "language" for intelligent
systems, in favor of having A.I. forced to reason in human language.

The core concepts behind its design are:

 1. Minimal Core, Maximal Composability
    The instruction set consist of the 16 truth-table operations
 2. High Compatibility
    io.Reader and io.Writer interface compliance make it compatible
    with file systems, network connections, and most other
    I/O infrastructure.
 3. High Portability
    As much as possible is carried in-band, balanced against
    hardware symphathy.
*/
type Value [Words]uint64

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

var valuePool = sync.Pool{
	New: func() any {
		val := &Value{}
		val.SetValueID(atomic.AddUint64(&globalValueIDCounter, 1))
		return val
	},
}

func NewValue() *Value {
	return valuePool.Get().(*Value)
}

/*
Read implements io.Reader, which serves two main purposes.
 1. It contributes to the High Compatibility core concept.
 2. It allows Values to pass through Values, which is the
    closest equivalent of an instruction-pointer.

It is important to understand that we do not pay any
traditional serialization tax, because the Value is already
serialized in memory.
*/
func (value *Value) Read(p []byte) (int, error) {
	if value[StateSlotIndex] == 0 {
		return 0, io.EOF
	}

	valueTo(value, p)
	return ByteSize, io.EOF
}

/*
Consume serializes the frame into p, promotes NextValueID to ValueID, and clears
the words for pipeline reuse (previous Read behavior).
*/
func (value *Value) Consume(p []byte) (int, error) {
	if value[StateSlotIndex] == 0 {
		return 0, io.EOF
	}

	valueTo(value, p)

	nextID := value.NextValueID()

	for i := range Words {
		value[i] = 0
	}

	value.SetValueID(nextID)

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
		slot := value[StateSlotIndex]
		seqIndex := value[StateSeqIndex]
		accumulator := value[StateAccumulator]

		// Stop if Region 0 is physically full.
		if slot >= Region0TokenCount {
			if bytesConsumed == 0 {
				return 0, ErrRegion0Full
			}

			return bytesConsumed, nil
		}

		token := Tokenize(b, seqIndex)
		existing := value[slot]

		switch existing {
		case token:
			bytesConsumed++
			accumulator ^= token
		case 0:
			value[slot] = token
			bytesConsumed++
			accumulator ^= token
		default:
			return bytesConsumed, nil
		}

		value[StateSeqIndex] = seqIndex + 1

		// Shift by 32 to check the chaotic byte-data, not the sequence index.
		if (accumulator>>32)&SignalMask == 0 {
			value[StateSeqIndex] = 0
		}

		value[StateSlotIndex] = slot + 1
		value[StateAccumulator] = accumulator
	}

	return bytesConsumed, nil
}

/*
Close implements io.Closer, and must be called when a Value
is discarded. It guarantees a sane exist from the substrate
and returns the value to the value pool.
*/
func (value *Value) Close() error {
	// Check value for any relationships
	for _, index := range []int{
		Region0PrevValueIDIndex, Region0NextValueIDIndex,
	} {
		if value[index] != 0 {
			// Decouple the previous value from this one
		}
	}

	valuePool.Put(value)
	return nil
}

func (value *Value) String() string {
	var builder strings.Builder

	for i := range Region0TokenCount {
		builder.WriteByte(byte(value[i] >> 32))
	}

	return builder.String()
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

/*
ApplyWireFrame copies a single serialized 1024-byte frame into the Value (the inverse
of ValueToBytes). Use this for bulk frame transfer; Value.Write tokenizes a byte
stream and is not appropriate for raw wire frames.
*/
func (v *Value) ApplyWireFrame(p []byte) error {
	if len(p) != ByteSize {
		return fmt.Errorf("primitive: wire frame length %d, want %d", len(p), ByteSize)
	}
	valueFrom(p, v)
	return nil
}

// DecodeTokensToText extracts the byte portion (upper 32 bits) of each
// non-zero TokenID in Region 0 and returns it as a printable string.
func DecodeTokensToText(v *Value) string {
	var b []byte
	for i := range Region0TokenCount {
		tok := v[i]
		if tok == 0 {
			break
		}
		ch := byte(tok >> 32)
		if ch >= 32 && ch < 127 {
			b = append(b, ch)
		} else {
			b = append(b, '.')
		}
	}
	return string(b)
}

type ValueErrorType string

const (
	ValueErrorTypeDivergence ValueErrorType = "divergence"
)

type ValueError struct {
	Err error
	Msg string
}

func NewValueError(err ValueErrorType) *ValueError {
	return &ValueError{Err: errors.New(string(err)), Msg: string(err)}
}

func (e *ValueError) Error() string {
	return fmt.Sprintf("%s: %s", e.Err, e.Msg)
}

// AffinityMask returns a 64-bit affinity pattern derived from the Value's content.
// This is used for fast topological clustering via bitwise operations in Region 2.
func (value *Value) AffinityMask() uint64 {
	// Create a simple but effective affinity signature from the first 8 tokens.
	// In a more sophisticated version, this could be a learned hash or bloom filter.
	hash := uint64(0)
	for i := 0; i < 8 && i < Region0TokenCount; i++ {
		if value[i] != 0 {
			// Mix the token with its position for better distribution
			hash = hash*31 + value[i]
			hash ^= (uint64(i) << 32)
		}
	}
	return hash
}

// SetAffinityMask writes a pattern into the affinity region (Region 2).
func (value *Value) SetAffinityMask(mask uint64) {
	// For now we just store it in the first word of the region.
	// In a fuller implementation this would write across multiple words.
	bitPos := RegionAffinityStart
	word := bitPos / 64
	if word < len(*value) {
		(*value)[word] = mask
	}
}

// InitializeAffinity sets a reasonable affinity mask based on the Value's content.
// This should be called after writing data to enable topological clustering.
func (value *Value) InitializeAffinity() {
	mask := value.AffinityMask()
	value.SetAffinityMask(mask)
}

// SetLink sets a link pointer in Region 4 for temporary grouping or next pointers.
func (value *Value) SetLink(linkID uint64) {
	bitPos := RegionLinkStart
	word := bitPos / 64
	if word < len(*value) {
		(*value)[word] = linkID
	}
}

// Link returns the link pointer from Region 4.
func (value *Value) Link() uint64 {
	bitPos := RegionLinkStart
	word := bitPos / 64
	if word < len(*value) {
		return (*value)[word]
	}
	return 0
}

// ProgramOp reads a 4-bit opcode from the program region at program counter pc.
func (value *Value) ProgramOp(pc int) uint8 {
	if pc < 0 || pc >= 64 {
		return 0
	}
	bitPos := RegionProgramStart + (pc * 4)
	// For now we read directly from the word array (simple implementation)
	word := bitPos / 64
	shift := uint(bitPos % 64)
	if word >= len(*value) {
		return 0
	}
	return uint8(((*value)[word] >> shift) & 0xF)
}

// SetProgramOp writes a 4-bit opcode into the program at position pc.
func (value *Value) SetProgramOp(pc int, op uint8) {
	if pc < 0 || pc >= 64 {
		return
	}
	bitPos := RegionProgramStart + (pc * 4)
	word := bitPos / 64
	shift := uint(bitPos % 64)
	if word >= len(*value) {
		return
	}

	mask := uint64(0xF) << shift
	(*value)[word] &^= mask
	(*value)[word] |= uint64(op) << shift
}

// InstallShatterProgram installs a simple shatter program (AND, A&^B, B&^A, HALT).
func (value *Value) InstallShatterProgram() {
	// Tick 0: AND (shared label)
	value.SetProgramOp(0, 0b1000)
	// Tick 1: A AND NOT B
	value.SetProgramOp(1, 0b0010)
	// Tick 2: B AND NOT A
	value.SetProgramOp(2, 0b0100)
	// Tick 3: HALT
	value.SetProgramOp(3, 0b0000)
}
