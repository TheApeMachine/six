package primitive

import (
	"io"
	"math/bits"
	"unsafe"
)

/*
Instruction Set Architecture (ISA)
Instead of arbitrary string opcodes, the instructions ARE points on the
prime lattice. The cantilever self-reconfigures by matching the prime
structure of incoming control frames against these known structural states.
*/
var (
	InstrContradiction          = NewValue()
	InstrNOR                    = NewValue()
	InstrConverseNonimplication = NewValue()
	InstrAND                    = NewValue()
	InstrNOT                    = NewValue()
	InstrAndNot                 = NewValue()
	InstrNotSecond              = NewValue()
	InstrXOR                    = NewValue()
	InstrNAND                   = NewValue()
	InstrXNOR                   = NewValue()
	InstrIdentityB              = NewValue()
	InstrMaterialConditional    = NewValue()
	InstrClear                  = NewValue()
	InstrConverseImplication    = NewValue()
	InstrOR                     = NewValue()
	InstrTautology              = NewValue()
	InstrMotorApply             = NewValue()
	InstrMotorInvert            = NewValue()
	InstrMotorCompose           = NewValue()
)

func init() {
	// The Instruction Set Architecture (ISA) is mapped directly to the universal
	// 16-row Truth Table as defined in NEXTEST.md.
	// We do not invent arbitrary opcodes. The mathematical definition of the
	// boolean operation IS its structural index.

	InstrContradiction.Set(0)          // Truth Table 0000 (0)
	InstrNOR.Set(1)                    // Truth Table 0001 (~(A | B))
	InstrConverseNonimplication.Set(2) // Truth Table 0010 (~A & B)
	InstrNOT.Set(3)                    // Truth Table 0011 (~A)
	InstrAndNot.Set(4)                 // Truth Table 0100 (A & ~B)
	InstrNotSecond.Set(5)              // Truth Table 0101 (~B)
	InstrXOR.Set(6)                    // Truth Table 0110 (A ^ B)
	InstrNAND.Set(7)                   // Truth Table 0111 (~(A & B))
	InstrAND.Set(8)                    // Truth Table 1000 (A & B)
	InstrXNOR.Set(9)                   // Truth Table 1001 (~(A ^ B))
	InstrIdentityB.Set(10)             // Truth Table 1010 (B)
	InstrMaterialConditional.Set(11)   // Truth Table 1011 (~A | B)
	InstrClear.Set(12)                 // Truth Table 1100 (A)
	InstrConverseImplication.Set(13)   // Truth Table 1101 (A | ~B)
	InstrOR.Set(14)                    // Truth Table 1110 (A | B)
	InstrTautology.Set(15)             // Truth Table 1111 (1)

	// Clear acts as a pipeline flush, resetting the stream to pure pass-through.
	// Truth Table 12 is Identity A (it returns exactly what it was given).

	// Motor operations are system-owned transforms outside the 16 truth-table rows.
	// 16 applies the first operand's derived motor to the second operand's bits.
	InstrMotorApply.Set(16)

	// 17 applies the inverse of the first operand's derived motor to the second operand.
	InstrMotorInvert.Set(17)

	// 18 composes motor(A) then motor(B), and applies the composed motor to B.
	InstrMotorCompose.Set(18)
}

const (
	Words    = 128
	ByteSize = Words * 8
	CoreBits = 8191
	LastMask = (1 << (CoreBits % 64)) - 1

	// InstructionMask uses the 64th bit of the final uint64 word (bit index 8191).
	// Because LastMask only covers bits 0-62, this bit sits strictly outside
	// the GF(8191) prime lattice and is safe to use for in-band signaling.
	InstructionMask uint64 = 1 << 63
)

/*
ValueError is a typed error for Value operations.
*/
type ValueError string

const (
	ErrShortValue ValueError = "value: buffer shorter than 1024 bytes"
)

/*
Error implements the error interface for ValueError.
*/
func (valueErr ValueError) Error() string {
	return string(valueErr)
}

/*
Value is the native programmable type. 8191-bit prime-indexed field
packed as 128 uint64 words. Each bit k represents the k-th prime.
The bit pattern is simultaneously a square-free integer (product of
active primes), a point on the divisibility lattice, and an affine
motor f(p) = scale·p + translate (mod 8191) derived from the field.

As a named array type it is a value type: copy by assignment, slice
with v[:], pass sub-ranges to operations with v[0:4]. No struct, no
pointer, no hidden fields — the type IS the memory layout.
*/
type Value [Words]uint64

var (
	valueTo   func(*Value, []byte)
	valueFrom func([]byte, *Value)
)

func init() {
	x := uint16(1)

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

/*
valueToPortable packs each uint64 word into 8 little-endian bytes.
Used on big-endian architectures where a raw memcpy would swap byte order.
*/
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

/*
valueFromPortable unpacks 8 little-endian bytes per word into uint64s.
Used on big-endian architectures where a raw memcpy would swap byte order.
*/
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
NewValueFromBytes returns a pointer to a Value initialized from a byte slice.
*/
func NewValueFromBytes(p []byte) *Value {
	value := NewValue()
	valueFrom(p, value)
	return value
}

/*
Read serializes the field into p as 1024 bytes. On little-endian
architectures this is a single copy/memmove.
*/
func (value *Value) Read(p []byte) (int, error) {
	if len(p) < ByteSize {
		return 0, ErrShortValue
	}

	valueTo(value, p)

	return ByteSize, io.EOF
}

/*
Write deserializes 1024 bytes into the field. On little-endian
architectures this is a single copy/memmove.
*/
func (value *Value) Write(p []byte) (int, error) {
	if len(p) < ByteSize {
		return 0, ErrShortValue
	}

	valueFrom(p, value)
	value[Words-1] &= LastMask

	return ByteSize, nil
}

/*
Close satisfies io.Closer for pipeline composition. Value is an in-memory frame,
so there is no external resource to release.
*/
func (value *Value) Close() error {
	return nil
}

/*
IsInstruction checks if the 8192nd bit is set, marking this Value
as an in-band control frame rather than standard data.
*/
func (value *Value) IsInstruction() bool {
	return (value[Words-1] & InstructionMask) != 0
}

/*
SetInstruction flags the Value as an executable command for the
transport layer by setting the out-of-band bit.
*/
func (value *Value) SetInstruction(instr *Value) {
	*value = *instr
	value[Words-1] |= InstructionMask
}

/*
ClearInstruction strips the command flag, returning the Value to a
pure mathematical state within the GF(8191) prime field.
*/
func (value *Value) ClearInstruction() {
	value[Words-1] &= ^InstructionMask
}

/*
Set activates bit p in the field.
*/
func (value *Value) Set(p int) {
	value[p/64] |= 1 << (p % 64)
}

/*
Has reports whether bit p is active.
*/
func (value *Value) Has(p int) bool {
	return value[p/64]&(1<<(p%64)) != 0
}

/*
Clamp zeroes the unused bit above CoreBits in the last word.
Call after any raw []uint64 operation that may have set bit 8191.
*/
func (value *Value) Clamp() {
	value[Words-1] &= LastMask
}

/*
PopCount returns the number of active bits in the core field.
*/
func (value *Value) PopCount() int {
	count := 0

	for i := range Words - 1 {
		count += bits.OnesCount64(value[i])
	}

	count += bits.OnesCount64(value[Words-1] & LastMask)

	return count
}

/*
IsZero reports whether every core bit is zero.
*/
func (value *Value) IsZero() bool {
	for i := range Words - 1 {
		if value[i] != 0 {
			return false
		}
	}

	return value[Words-1]&LastMask == 0
}

/*
Equal reports whether two Values have identical fields.
*/
func (value *Value) Equal(other *Value) bool {
	return *value == *other
}
