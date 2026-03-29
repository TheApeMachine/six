//go:generate go run gen.go
package primitive

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
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

	STATE (words 60–62)
	Slot index, sequence index, XOR accumulator for the Write engine.

	AFFINITY (word 63)
	64-bit mask for topological clustering via bitwise AND.

	REGISTERS r0–r9, fw (words 64–74)
	General-purpose registers for native programs and scratch spans.

	PC (word 75)
	Program counter. Writable by programs for jumps.

	PROGRAM (words 76–127)
	52 words = 3328 bits = 104 × 32-bit instruction slots.
	Native programs execute all 16 two-input boolean truth tables.
	Conditional skip-if-zero enables loops without invented opcodes.
*/

const (
	// Compile-time constants needed for the Value type definition.
	Words    = 128
	ByteSize = Words * 8

	// SignalMask is the mechanical bit-pattern used to reset the sequence index.
	SignalMask = 0xF

	// ExecStatusWord is the word reserved for kernel exit markers (high 16 bits);
	// must match generated pkg/compute/kernel/shared/primitives.h EXEC_STATUS_WORD.
	ExecStatusWord  = 63
	ExecStatusShift = 48
)

// Exit reasons written into ExecStatusWord by CPU / GPU kernels (high 16 bits).
const (
	ExecExitNone uint16 = iota
	ExecExitExhausted
	ExecExitHaltOpcode
	ExecExitBadProgramWord
)

var (
	valueTo   func(*Value, []byte)
	valueFrom func([]byte, *Value)

	globalValueIDCounter uint64 = 1000000 // Start high to avoid clashing with Backend
)

type valueLifecycle struct {
	refs   atomic.Int64
	pooled atomic.Bool
}

var valueLifecycles sync.Map // map[uintptr]*valueLifecycle

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

/*
valuePool is a global pool of Values. It is used to recycle
Values that have been closed. It should be safe to re-use ValueIDs
because the Tomstone program should be clearing up any references to
a discarded ValueID.
*/
var valuePool = sync.Pool{
	New: func() any {
		val := &Value{}
		val.SetValueID(atomic.AddUint64(&globalValueIDCounter, 1))
		val.installFirmware(core.FirmwareTypeBootloader)
		return val
	},
}

/*
NewValue should only be used to create the initial Value.
This method should not be used to create temporary Values.
*/
func NewValue(p []byte) (*Value, error) {
	value := valuePool.Get().(*Value)
	registerPooledLifecycle(value)

	// Reinitialize the pooled frame so prompt construction never inherits stale
	// bytes from a previous temporary Value.
	for i := range value {
		value[i] = 0
	}
	value.SetValueID(atomic.AddUint64(&globalValueIDCounter, 1))
	value.installFirmware(core.FirmwareTypeBootloader)

	tokenWords := int((core.Cfg.TokenBits + 63) / 64)
	if tokenWords <= 0 {
		return nil, errnie.Error(
			NewValueError(ValueErrorFailedToken),
		)
	}

	if len(p) > tokenWords {
		p = p[:tokenWords]
	}

	for i, b := range p {
		if !value.SetTokenID(core.Cfg.TokenIndex+i, Tokenize(b, uint64(i))) {
			return nil, errnie.Error(
				NewValueError(ValueErrorFailedToken),
			)
		}
	}

	return value, nil
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
	if len(p) < ByteSize {
		return 0, io.ErrShortBuffer
	}

	valueTo(value, p)
	return ByteSize, io.EOF
}

/*
Write implements io.Writer. It is how Values are "folded" through each
other, which acts as the closes thing to an instruction pointer.
*/
func (value *Value) Write(p []byte) (int, error) {
	incoming := valuePool.Get().(*Value)
	defer valuePool.Put(incoming)

	valueFrom(p, incoming)

	if value.HasProgram() {
		if err := compute.UniversalBitwise(
			unsafe.Pointer(value),
			unsafe.Pointer(incoming),
		); err != nil {
			return 0, err
		}
	} else if incoming.HasProgram() {
		if err := compute.UniversalBitwise(
			unsafe.Pointer(incoming),
			unsafe.Pointer(value),
		); err != nil {
			return 0, err
		}
	}

	valueTo(incoming, p)

	return len(p), nil
}

/*
Close implements io.Closer, and must be called when a Value
is discarded. It guarantees a sane exist from the substrate
and returns the value to the value pool. This is not meant
as a quick-and-dirty way to discard a Value, that has to
be done by loading the tombstone firmware.
*/
func (value *Value) Close() error {
	return value.Release()
}

// Retain increments the external reference count for pooled Values.
// For unmanaged Values (e.g. stack / raw overlays) this is a no-op.
func (value *Value) Retain() int64 {
	lc, ok := lifecycleFor(value)
	if !ok {
		return 1
	}

	for {
		current := lc.refs.Load()
		if current <= 0 {
			return current
		}
		if lc.refs.CompareAndSwap(current, current+1) {
			return current + 1
		}
	}
}

// Release decrements the external reference count for pooled Values and
// returns them to the pool only after the final owner releases.
func (value *Value) Release() error {
	lc, ok := lifecycleFor(value)
	if !ok {
		return nil
	}

	refs := lc.refs.Add(-1)
	if refs > 0 {
		return nil
	}
	if refs < 0 {
		lc.refs.Store(0)
		return fmt.Errorf("primitive.Value.Release: refcount underflow for value id=%d", value.ValueID())
	}

	if lc.pooled.Load() {
		if value.shouldRecycleFromExecStatus() || value.isPoolReusable() {
			value.resetForPool()
			valuePool.Put(value)
			return nil
		}
	}

	// If this frame cannot be safely reused, drop lifecycle tracking so it can
	// be reclaimed naturally by GC.
	valueLifecycles.Delete(valueLifecycleKey(value))
	return nil
}

func (value *Value) shouldRecycleFromExecStatus() bool {
	switch value.ExecExitCode() {
	case ExecExitHaltOpcode, ExecExitExhausted, ExecExitBadProgramWord:
		return true
	default:
		return false
	}
}

func (value *Value) resetForPool() {
	for i := range value {
		value[i] = 0
	}
}

func (value *Value) isPoolReusable() bool {
	wiped := true

	// Cfg *Bits fields are bit widths; convert to word spans before indexing value[i].
	checkRegion := func(start int, bits uint64) {
		if !wiped {
			return
		}
		n := int((bits + 63) / 64)
		for i := 0; i < n; i++ {
			idx := start + i
			if idx < 0 || idx >= Words {
				continue
			}
			if value[idx] != 0 {
				wiped = false
				return
			}
		}
	}

	checkRegion(core.Cfg.TokenIndex, core.Cfg.TokenBits)
	checkRegion(core.Cfg.AffinityIndex, core.Cfg.AffinityBits)
	checkRegion(core.Cfg.ProgramIndex, core.Cfg.ProgramBits)

	return wiped
}

/*
installFirmware installs the firmware into the Value. This should
ideally only be used to install the bootloader firmware. In all
other cases, setting the fw register to the firmware index and
setting the pc register to the program index (which is where)
the bootloader starts) is the correct way to install firmware.
*/
func (value *Value) installFirmware(fw core.FirmwareType) {
	prog := core.Cfg.Firmware[fw]
	wordBase := uint64(core.Cfg.ProgramIndex)

	for i := 0; i < len(prog); i += 2 {
		wordPos := wordBase + uint64(i/2)

		if int(wordPos) >= Words {
			break
		}

		var w uint64
		w = uint64(prog[i])

		if i+1 < len(prog) {
			w |= uint64(prog[i+1]) << 32
		}

		value[wordPos] = w
	}
}

func (value *Value) String() string {
	var builder strings.Builder
	n := int((core.Cfg.TokenBits + 63) / 64)
	for i := 0; i < n; i++ {
		idx := core.Cfg.TokenIndex + i
		if idx >= Words {
			break
		}
		builder.WriteByte(byte(value[idx] >> 32))
	}

	return strings.TrimRight(builder.String(), "\x00")
}

// TraceString implements errnie.TraceStringer for compact trace / log output.
func (v *Value) TraceString() string {
	if v == nil {
		return "<nil>"
	}
	tok := v.String()
	r := []rune(tok)
	if len(r) > errnie.TraceTokenPreviewRunes {
		tok = string(r[:errnie.TraceTokenPreviewRunes]) + "…"
	}
	return fmt.Sprintf("id=%d tokens=%s", v.ValueID(), strconv.Quote(tok))
}

/*
Tokenize securely packs a byte and its sequence index into a 64-bit hardware token.
*/
func Tokenize(b byte, index uint64) uint64 {
	return (uint64(b) << 32) | index
}

func region0TokenIndexOK(index int) bool {
	tokenWords := int((core.Cfg.TokenBits + 63) / 64)
	hi := core.Cfg.TokenIndex + tokenWords
	return index >= core.Cfg.TokenIndex && index < hi
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
	n := int((core.Cfg.TokenBits + 63) / 64)
	out := make([]uint64, n)
	for i := 0; i < n; i++ {
		idx := core.Cfg.TokenIndex + i
		if idx >= Words {
			break
		}
		out[i] = value[idx]
	}
	return out
}

func (value *Value) SetTokenIDs(tokens []uint64) int {
	nWords := int((core.Cfg.TokenBits + 63) / 64)
	n := min(len(tokens), nWords)
	for i := 0; i < n; i++ {
		idx := core.Cfg.TokenIndex + i
		if idx >= Words {
			break
		}
		value[idx] = tokens[i]
	}
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

func registerPooledLifecycle(value *Value) {
	if value == nil {
		return
	}

	key := valueLifecycleKey(value)
	entryAny, _ := valueLifecycles.LoadOrStore(key, &valueLifecycle{})
	entry := entryAny.(*valueLifecycle)
	entry.pooled.Store(true)
	entry.refs.Store(1)
}

func lifecycleFor(value *Value) (*valueLifecycle, bool) {
	if value == nil {
		return nil, false
	}
	entry, ok := valueLifecycles.Load(valueLifecycleKey(value))
	if !ok {
		return nil, false
	}
	return entry.(*valueLifecycle), true
}

func valueLifecycleKey(value *Value) uintptr {
	return uintptr(unsafe.Pointer(value))
}

func (value *Value) Clone() *Value {
	clone := valuePool.Get().(*Value)
	for i := range value {
		clone[i] = value[i]
	}
	registerPooledLifecycle(clone)
	clone.SetValueID(atomic.AddUint64(&globalValueIDCounter, 1))
	// Do not blindly install bootloader on a clone, we want to clone the exact state!
	return clone
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

// ClearExecExitCode clears the kernel exit marker in the high bits of ExecStatusWord.
func (value *Value) ClearExecExitCode() {
	value[ExecStatusWord] &= 0x0000FFFFFFFFFFFF
}

// SetExecExitCode records why a native kernel stopped (high 16 bits of ExecStatusWord).
func (value *Value) SetExecExitCode(code uint16) {
	value[ExecStatusWord] = (value[ExecStatusWord] & 0x0000FFFFFFFFFFFF) | (uint64(code) << ExecStatusShift)
}

// ExecExitCode returns the last kernel exit marker, if any.
func (value *Value) ExecExitCode() uint16 {
	return uint16(value[ExecStatusWord] >> ExecStatusShift)
}

/*
DecodeTokensToText extracts the byte portion (upper 32 bits) of each
non-zero TokenID in Region 0 and returns it as a printable string.
*/
func DecodeTokensToText(v *Value) string {
	var b []byte
	n := int((core.Cfg.TokenBits + 63) / 64)
	for i := 0; i < n; i++ {
		idx := core.Cfg.TokenIndex + i
		if idx >= Words {
			break
		}
		tok := v[idx]
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

// ProgramOp returns the 4-bit opcode at instruction slot i in the in-band
// program region, or 0 if i is out of range.
func (value *Value) ProgramOp(slot int) uint8 {
	if slot < 0 || slot >= core.Cfg.MaxPC {
		return 0
	}
	wordBase := uint64(core.Cfg.ProgramIndex)
	pc := uint64(slot)
	wordPos := wordBase + (pc / 2)
	if int(wordPos) >= Words {
		return 0
	}
	shift := uint((pc % 2) * 32)
	instr := uint32(value[wordPos] >> shift)
	return uint8(instr & 0xF)
}

func (value *Value) HasProgram() bool {
	// If the firmware register is set, it signals a program should be loaded
	if value[core.Cfg.FW] > 0 {
		return true
	}
	startWord := core.Cfg.ProgramIndex
	nProgWords := int((core.Cfg.ProgramBits + 63) / 64)
	endWord := startWord + nProgWords
	if startWord < 0 {
		startWord = 0
	}
	if endWord > Words {
		endWord = Words
	}
	for i := startWord; i < endWord; i++ {
		if value[i] != 0 {
			return true
		}
	}
	return false
}

type ValueErrorType string

const (
	ValueErrorFailedToken ValueErrorType = "failed_token"
	ValueErrorDivergence  ValueErrorType = "divergence"
	ValueErrorDataFull    ValueErrorType = "data_full"
)

type ValueError struct {
	Err error
}

func NewValueError(err ValueErrorType) *ValueError {
	return &ValueError{Err: errors.New(string(err))}
}

func (e *ValueError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return "value error"
}

func (e *ValueError) Unwrap() error {
	return e.Err
}
