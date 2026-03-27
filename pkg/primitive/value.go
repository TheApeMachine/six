//go:generate go run gen.go
package primitive

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
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

	STATE (words 61–63)
	Slot index, sequence index, XOR accumulator for the Write engine.

	AFFINITY (word 64)
	64-bit mask for topological clustering via bitwise AND.

	LINK (word 65)
	64-bit temporary grouping pointer.

	GOSSIP (words 66–69)
	256-bit routing signature for GF(65537) mesh traversal.

	TTL (word 70, low 8 bits)
	Hop counter decremented by each Region.

	REGISTERS r0–r6 (words 71–77)
	General-purpose registers for native programs.

	PC (word 78)
	Program counter. Writable by programs for jumps.

	PROGRAM (words 79–127)
	49 words = 3136 bits = 98 × 32-bit instruction slots.
	Native programs execute all 16 two-input boolean truth tables.
	Conditional skip-if-zero enables loops without invented opcodes.
*/

const (
	// Compile-time constants needed for the Value type definition.
	Words    = 128
	ByteSize = Words * 8

	// SignalMask is the mechanical bit-pattern used to reset the sequence index.
	SignalMask = 0xF
)

var (
	valueTo   func(*Value, []byte)
	valueFrom func([]byte, *Value)

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

func NewValue(p ...[]byte) *Value {
	value := valuePool.Get().(*Value)
	if len(p) > 0 && len(p[0]) > 0 {
		valueFrom(p[0], value)
	}
	return value
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
	if value[core.Cfg.StateIndex] == 0 {
		return 0, io.EOF
	}

	if len(p) < ByteSize {
		return 0, io.ErrShortBuffer
	}

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
		slot := value[core.Cfg.StateIndex]
		seqIndex := value[core.Cfg.StateSequence]
		accumulator := value[core.Cfg.StateAccumulator]

		// Stop if Region 0 is physically full.
		if !region0TokenIndexOK(int(slot)) {
			if bytesConsumed == 0 {
				return 0, NewValueError(ValueErrorDataFull)
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

		value[core.Cfg.StateSequence] = seqIndex + 1

		// Shift by 32 to check the chaotic byte-data, not the sequence index.
		if (accumulator>>32)&SignalMask == 0 {
			value[core.Cfg.StateSequence] = 0
		}

		value[core.Cfg.StateIndex] = slot + 1
		value[core.Cfg.StateAccumulator] = accumulator
	}

	return bytesConsumed, nil
}

/*
Close implements io.Closer, and must be called when a Value
is discarded. It guarantees a sane exist from the substrate
and returns the value to the value pool.
*/
func (value *Value) Close() error {
	vid := value.ValueID()
	for i := range Words {
		value[i] = 0
	}
	value.SetValueID(vid)
	valuePool.Put(value)
	return nil
}

func (value *Value) String() string {
	var builder strings.Builder

	for i := range core.Cfg.TokenIndex {
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
	return index >= 0 && index < int(core.Cfg.TokenIndex)
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
	tokens := make([]uint64, core.Cfg.TokenBits)
	copy(tokens, value[:core.Cfg.TokenBits])
	return tokens
}

func (value *Value) SetTokenIDs(tokens []uint64) int {
	n := min(len(tokens), int(core.Cfg.TokenBits))
	copy(value[:n], tokens[:n])
	return n
}

func (value *Value) ValueID() uint64 {
	return value[core.Cfg.ValueID]
}

func (value *Value) ID() string {
	return fmt.Sprintf("%d", value.ValueID())
}

func (value *Value) SetValueID(id uint64) {
	value[core.Cfg.ValueID] = id
}

func (value *Value) PrevValueID() uint64 {
	return value[core.Cfg.PreviousID]
}

func (value *Value) SetPrevValueID(id uint64) {
	value[core.Cfg.PreviousID] = id
}

func (value *Value) NextValueID() uint64 {
	return value[core.Cfg.NextID]
}

func (value *Value) SetNextValueID(id uint64) {
	value[core.Cfg.NextID] = id
}

/*
BytesToValue overlays a 1024-byte slice onto the Value pointer via unsafe memory access.
*/
func BytesToValue(p []byte) *Value {
	var v Value
	valueFrom(p, &v)

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

/*
DecodeTokensToText extracts the byte portion (upper 32 bits) of each
non-zero TokenID in Region 0 and returns it as a printable string.
*/
func DecodeTokensToText(v *Value) string {
	var b []byte
	for i := range core.Cfg.TokenBits {
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
	ValueErrorDivergence ValueErrorType = "divergence"
	ValueErrorDataFull   ValueErrorType = "data_full"
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

func (value *Value) HasProgram() bool {
	// If the firmware register is set, it signals a program should be loaded
	if value[core.Cfg.FW] > 0 {
		return true
	}
	// Check if the program region has any non-zero words
	startWord := core.Cfg.ProgramIndex / 64
	endWord := (core.Cfg.ProgramIndex + int(core.Cfg.ProgramBits)) / 64
	for i := startWord; i < endWord && i < Words; i++ {
		if value[i] != 0 {
			return true
		}
	}
	return false
}
