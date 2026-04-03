//go:generate go run gen.go
package primitive

import (
	"errors"
	"fmt"
	"io"
	"math/bits"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/firmware"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
)

const (
	/*
		tokenIDPrime is a 64-bit prime (2^64-59) so TokenID maps
		(multiplier*index + byte) mod prime across the full uint64 range.
	*/
	tokenIDPrime      = uint64(18446744073709551557)
	tokenIDMultiplier = uint64(6364136223846793005)
)

var tokenIDMulInverse uint64

const detokenizeCacheSize = 4096

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

	valueTokenIDs sync.Map

	globalValueIDCounter uint64 = 1000000 // Start high to avoid clashing with Backend
)

/*
persistTokenIDsByValueID stores the reversible TokenIDs generated during
constructor-time tokenization for exact reverse lookup during decode.
*/
func persistTokenIDsByValueID(value *Value, tokenIDs []uint64) {
	if value == nil {
		return
	}

	valueID := value.GetWord(core.Cfg.Value.Region.ID.Start)
	if valueID == 0 {
		return
	}

	if len(tokenIDs) == 0 {
		valueTokenIDs.Store(valueID, []uint64(nil))
		return
	}

	cached := make([]uint64, len(tokenIDs))
	copy(cached, tokenIDs)

	valueTokenIDs.Store(valueID, cached)
}

/*
valueTokenIDsForLookup resolves constructor-time TokenIDs for a ValueID.
*/
func valueTokenIDsForLookup(valueID uint64) []uint64 {
	raw, ok := valueTokenIDs.Load(valueID)
	if !ok {
		return nil
	}

	if tokenIDs, ok := raw.([]uint64); ok {
		return tokenIDs
	}

	return nil
}

/*
discardTokenIDsByValueID clears constructor-time token metadata for a ValueID.
*/
func discardTokenIDsByValueID(valueID uint64) {
	if valueID == 0 {
		return
	}

	valueTokenIDs.Delete(valueID)
}

/*
DiscardTokenIDsByValueID is the exported variant for callers outside the
primitive package (e.g. backend.handleFollowUp) that need to release token
metadata for frames exiting the pipeline without a full Close().
*/
func DiscardTokenIDsByValueID(valueID uint64) {
	discardTokenIDsByValueID(valueID)
}

/*
ReleaseFrame zeros the frame, discards its token metadata, and returns
it to the value pool. Use this instead of letting frames fall to the GC
when they exit the pipeline without a Close() call (e.g. dropped
follow-ups in the backend).
*/
func ReleaseFrame(frame *[128]uint64) {
	if frame == nil {
		return
	}
	idWord := core.Cfg.Value.Region.ID.Start
	if idWord >= 0 && idWord < len(frame) {
		discardTokenIDsByValueID(frame[idWord])
	}
	for i := range frame {
		frame[i] = 0
	}
	// Cast the original pointer back to *Value rather than copying the
	// frame onto the stack. Value is [128]uint64, so the types have
	// identical layout — this avoids leaking the heap allocation.
	valuePool.Put((*Value)(frame))
}

/*
ValueTokenIDsForLookup returns cached TokenIDs captured during NewValue.
*/
func ValueTokenIDsForLookup(valueID uint64) []uint64 {
	return valueTokenIDsForLookup(valueID)
}

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
type Value [128]uint64

/*
ValueFrameReader provides the minimal shape to walk linked frames by ValueID.
*/
type ValueFrameReader interface {
	FrameByValueID(valueID uint64) ([128]uint64, bool)
}

/*
Walk traverses the linked list of frames starting at value by following NextID.
It is index-agnostic and only uses in-band pointers.
*/
func (value *Value) Walk(index ValueFrameReader, visit func(valueID uint64, frame [128]uint64) bool) {
	if value == nil || index == nil {
		return
	}

	seen := make(map[uint64]struct{}, 8)
	cursor := value.GetWord(core.Cfg.Value.Region.ID.Start)

	for cursor != 0 {
		if _, exists := seen[cursor]; exists {
			return
		}

		seen[cursor] = struct{}{}

		frame, ok := index.FrameByValueID(cursor)
		if !ok {
			return
		}

		if visit != nil && !visit(cursor, frame) {
			return
		}

		cursor = frame[core.Cfg.Value.Region.Next.Start]
	}
}

/*
DecodeTokenIDs reverses affine TokenIDs into bytes and orders them by decoded
sequence index. It returns only tokens that match a valid detokenize candidate.
*/
func DecodeTokenIDs(tokenIDs []uint64) []byte {
	if len(tokenIDs) == 0 {
		return nil
	}

	maxIndex := uint64(1)
	if core.Cfg.Value.Region.Tokens.Bits > 0 {
		maxIndex = uint64(core.Cfg.Value.Region.Tokens.Bits / 8)
	}

	observed := make(map[uint64]byte, len(tokenIDs))
	for _, tokenID := range tokenIDs {
		b, idx, ok := DetokenizeTokenID(tokenID)
		if !ok {
			continue
		}

		if idx >= maxIndex {
			continue
		}

		if _, exists := observed[idx]; !exists {
			observed[idx] = b
		}
	}

	if len(observed) == 0 {
		return nil
	}

	indices := make([]uint64, 0, len(observed))
	for idx := range observed {
		indices = append(indices, idx)
	}

	sort.Slice(indices, func(a, b int) bool { return indices[a] < indices[b] })

	out := make([]byte, len(indices))
	for i, idx := range indices {
		out[i] = observed[idx]
	}

	return out
}

func init() {
	x := uint16(1)
	tokenIDMulInverse = modPowU64(tokenIDMultiplier, tokenIDPrime-2, tokenIDPrime)

	// Fast-path memory alignment for little-endian architectures
	if *(*byte)(unsafe.Pointer(&x)) == 1 {
		valueTo = func(v *Value, p []byte) {
			copy(p, unsafe.Slice((*byte)(unsafe.Pointer(&v[0])), core.Cfg.Value.Bytes))
		}
		valueFrom = func(p []byte, v *Value) {
			copy(unsafe.Slice((*byte)(unsafe.Pointer(&v[0])), core.Cfg.Value.Bytes), p)
		}
		return
	}

	valueTo = valueToPortable
	valueFrom = valueFromPortable
}

func valueToPortable(v *Value, p []byte) {
	for i := range core.Cfg.Value.Words {
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
	for i := range core.Cfg.Value.Words {
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
a discarded ValueID. Values should ALWAYS be tombstoned, never just
discarded. You will receive an error from Close if you try to close
a Value that has not been properly tombstoned.
*/
var valuePool = sync.Pool{
	New: func() any {
		return &Value{}
	},
}

func clearProgramWords(value *Value) {
	if value == nil {
		return
	}

	reg := core.Cfg.Value.Region.Program
	nWords := int((reg.Bits + 63) / 64)

	for offset := 0; offset < nWords; offset++ {
		index := reg.Start + offset

		if index < 0 || index >= len(*value) {
			continue
		}

		(*value)[index] = 0
	}
}

/*
NewValue should only be used to create the initial Value.
This method should not be used to create temporary Values.
*/
func NewValue(p []byte) (*Value, error) {
	value := valuePool.Get().(*Value)

	// Zero the entire frame first. sync.Pool may return a frame with
	// computational residue from its previous lifecycle — stale registers,
	// link pointers, state words, and scratch data that would corrupt the
	// new Value's execution if not wiped.
	for i := range value {
		value[i] = 0
	}

	// Always mint a fresh ValueID.
	value[core.Cfg.Value.Region.ID.Start] = atomic.AddUint64(
		&globalValueIDCounter,
		1,
	)

	clearProgramWords(value)

	if err := value.InstallFirmware(core.FirmwareTypeLearn); err != nil {
		return nil, errnie.Error(err)
	}

	// Payload span starts after the bootstrap prefix; introns survive because
	// they do not overlap the fixed learn kernel slots written above.
	firmware.InsertIntrons((*[128]uint64)(value), 8)

	tokenIDs := make([]uint64, 0, int(
		(core.Cfg.Value.Region.Tokens.Bits+63)/64,
	))
	seed := uint64(1)

	if len(p) > 0 {
		seed = uint64(p[0])
	}

	tokenWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)

	if tokenWords <= 0 {
		return nil, errnie.Error(
			NewValueError(ValueErrorFailedToken),
		)
	}

	// can hold more now since it's superimposed, but
	// let's bound it safely. TODO: Calculate Shannon
	// capacity and do not exceed > 40%.
	if len(p) > int(core.Cfg.Value.Region.Tokens.Bits/8) {
		p = p[:int(core.Cfg.Value.Region.Tokens.Bits/8)]
	}

	for idx, b := range p {
		tokenIDs = append(tokenIDs, Tokenize(b, uint64(idx)))
		value.LeftShiftTokens()
		value.BindTokenHD(b)

		if idx >= 1 {
			seed = (seed*31 + uint64(b)) & 0x1FFF
		}
	}

	if seed == 0 {
		seed = 1
	}

	value[core.Cfg.Value.Region.State.Sequence] = seed

	// Derive Affinity from raw data (Bloom filter of 3-byte n-grams) and
	// from the encoded Tokens region (SimHash LSH), then combine.
	bloom := ComputeAffinityBloom(p)
	value[core.Cfg.Value.Region.Affinity.Start] = bloom
	value.ComputeAffinityLSH() // overwrite with LSH; OR in Bloom bits
	value[core.Cfg.Value.Region.Affinity.Start] |= bloom
	value[core.Cfg.Value.Region.State.Accumulator] = value[core.Cfg.Value.Region.Affinity.Start]

	// Affine TokenIDs (Tokenize per byte) are what DetokenizeTokenID inverts. The
	// token *region words* are superposed HD state; using those as LSM keys
	// breaks reverse lookup (DecodeTokenIDs sees garbage / a lone stray match).
	persistTokenIDsByValueID(value, tokenIDs)

	return value, nil
}

/*
IsPrompt returns true if the Value is a prompt.
*/
func (value *Value) IsPrompt() bool {
	if value.GetWord(
		core.Cfg.Value.Region.Registers.FW,
	) == uint64(
		core.FirmwareTypePrompt,
	) {
		return true
	}

	return false
}

func (value *Value) InstallFirmware(
	firmwareType core.FirmwareType,
) error {
	if value == nil {
		return errnie.Error(
			NewValueError(ValueErrorFailedByteConversion),
		)
	}

	value[core.Cfg.Value.Region.Registers.FW] = uint64(firmwareType)

	clearProgramWords(value)

	for instructionIndex := range core.Cfg.Firmware[firmwareType] {
		// Two 32-bit LGP slots per 64-bit program word (UniversalBitwise layout).
		wordIndex := core.Cfg.Value.Region.Program.Start + instructionIndex/2

		if wordIndex < 0 || wordIndex >= len(*value) {
			return errnie.Error(
				NewValueError(ValueErrorInvalidProgramWord),
			)
		}

		instr := uint64(core.Cfg.Firmware[firmwareType][instructionIndex])
		shift := uint((instructionIndex % 2) * 32)
		mask := uint64(0xFFFFFFFF) << shift

		value[wordIndex] = (value[wordIndex] &^ mask) | (instr << shift)
	}

	value[core.Cfg.Value.Region.PC.Start] = uint64(
		core.Cfg.Value.Region.Program.Start,
	)

	return nil
}

/*
LeftShiftTokens performs a 1-bit circular left
shift on the entire Tokens region.
*/
func (value *Value) LeftShiftTokens() {
	nWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	var carry uint64 = 0

	for i := 0; i < nWords; i++ {
		idx := core.Cfg.Value.Region.Tokens.Start + i

		if idx >= core.Cfg.Value.Words {
			break
		}

		curr := value[idx]
		nextCarry := curr >> 63
		value[idx] = (curr << 1) | carry
		carry = nextCarry
	}

	if carry > 0 {
		idx := core.Cfg.Value.Region.Tokens.Start

		if idx < core.Cfg.Value.Words {
			value[idx] |= carry
		}
	}
}

/*
BindTokenHD XORs the static random 3648-bit FSM
signature for the given byte.
*/
func (value *Value) BindTokenHD(b byte) {
	nWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	sig := ByteSignatures[b]

	for i := 0; i < nWords && i < len(sig); i++ {
		idx := core.Cfg.Value.Region.Tokens.Start + i

		if idx >= core.Cfg.Value.Words {
			break
		}

		value[idx] ^= sig[i]
	}
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
	if len(p) < core.Cfg.Value.Bytes {
		return 0, io.ErrShortBuffer
	}

	valueTo(value, p)
	return core.Cfg.Value.Bytes, io.EOF
}

/*
Write implements io.Writer as a compatibility shim for the wire format.
We are moving away from the idea of folding Values, as it keeps things
needlessly slower than they could be. From now on Values will only
use their own data for computation, and they will be immutable, so any
computation (which will now be "signals") will emit new Values instead.
*/
func (value *Value) Write(p []byte) (int, error) {
	if len(p) < core.Cfg.Value.Bytes {
		return 0, io.ErrShortBuffer
	}

	valueFrom(p, value)
	return core.Cfg.Value.Bytes, nil
}

/*
Close implements io.Closer, and must be called when a Value
is discarded. It guarantees a sane exit from the substrate
and returns the value to the value pool. This is not meant
as a quick-and-dirty way to discard a Value, that has to
be done by loading the tombstone firmware.
*/
func (value *Value) Close() error {
	if err := value.isTombstoned(); err != nil {
		return errnie.Error(
			NewValueError(ValueErrorNotTombstoned),
		)
	}

	discardTokenIDsByValueID(value.GetWord(core.Cfg.Value.Region.ID.Start))
	valuePool.Put(value)
	return nil
}

func (value *Value) isTombstoned() error {
	if slices.Max(slices.Concat(
		value[core.Cfg.Value.Region.Tokens.Start:core.Cfg.Value.Region.Tokens.Start+int(
			core.Cfg.Value.Region.Tokens.Bits/64,
		)-3],
		value[core.Cfg.Value.Region.Affinity.Start:core.Cfg.Value.Region.Affinity.Start+int(
			core.Cfg.Value.Region.Affinity.Bits/64,
		)],
		value[core.Cfg.Value.Region.Program.Start:core.Cfg.Value.Region.Program.Start+int(
			core.Cfg.Value.Region.Program.Bits/64,
		)],
	)) > 0 {
		return errnie.Error(
			NewValueError(ValueErrorNotTombstoned),
		)
	}

	return nil
}

/*
Link sets the PrevID and NextID of the Value, which turns the
Values into a doubly linked list.
*/
func (value *Value) Link(prev, next uint64) {
	if value == nil {
		return
	}

	if prev != 0 {
		value.SetWord(
			core.Cfg.Value.Region.Prev.Start,
			prev,
		)
	}

	if next != 0 {
		value.SetWord(
			core.Cfg.Value.Region.Next.Start,
			next,
		)
	}
}

/*
String returns the Token region of the Value as a readable string.
It resolves TokenIDs in the LSM by exact Value-frame bitmap match, then
orders by the sequence index packed in each TokenID (Tokenize).
*/
func (value *Value) String() string {
	return fmt.Sprintf(
		"Value{%d}", value.GetWord(core.Cfg.Value.Region.ID.Start),
	)
}

func (value *Value) Bytes() ([]byte, error) {
	if value == nil {
		ve := NewValueError(ValueErrorFailedByteConversion)
		errnie.Error(ve, "nil value")

		return nil, ve
	}

	p := make([]byte, core.Cfg.Value.Bytes)

	if convErr := ValueToBytes(value, p); convErr != nil {
		ve := NewValueError(ValueErrorFailedByteConversion)
		errnie.Error(ve, convErr)

		return nil, errors.Join(ve, convErr)
	}

	return p, nil
}

/*
GetWord returns the word at the given index.
*/
func (value *Value) GetWord(index int) uint64 {
	if value == nil {
		return 0
	}

	if index < 0 || index >= len(*value) {
		return 0
	}

	return value[index]
}

/*
SetWord sets the word at the given index.
*/
func (value *Value) SetWord(index int, word uint64) {
	if value == nil {
		return
	}

	if index < 0 || index >= len(*value) {
		return
	}

	value[index] = word
}

/*
Tokenize packs a byte and sequence index into a 64-bit LSM key via affine
mixing mod tokenIDPrime so keys spread uniformly instead of clustering on
byte-high half-words.
*/
func Tokenize(b byte, index uint64) uint64 {
	hi, lo := bits.Mul64(tokenIDMultiplier, index)
	lo, carry := bits.Add64(lo, uint64(b), 0)
	hi, _ = bits.Add64(hi, 0, carry)
	_, rem := bits.Div64(hi, lo, tokenIDPrime)

	return rem
}

/*
DetokenizeTokenID reverses the affine Tokenize map.
It returns (byte, index, ok), where ok is true only for exact forward-match.
*/
func DetokenizeTokenID(tokenID uint64) (byte, uint64, bool) {
	for candidate := 0; candidate <= 255; candidate++ {
		diff := subModU64(tokenID, uint64(candidate), tokenIDPrime)
		index := modMulU64(diff, tokenIDMulInverse, tokenIDPrime)

		if index < 1<<32 {
			if Tokenize(byte(candidate), index) == tokenID {
				return byte(candidate), index, true
			}
		}
	}

	return 0, 0, false
}

func modMulU64(a, b, mod uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	_, rem := bits.Div64(hi, lo, mod)

	return rem
}

func modPowU64(base, exp, mod uint64) uint64 {
	res := uint64(1)
	for exp > 0 {
		if exp&1 == 1 {
			res = modMulU64(res, base, mod)
		}
		base = modMulU64(base, base, mod)
		exp >>= 1
	}

	return res
}

func subModU64(x, y, mod uint64) uint64 {
	if x >= y {
		return x - y
	}

	return mod - (y - x)
}

func region0TokenIndexOK(index int) bool {
	tokenWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	hi := core.Cfg.Value.Region.Tokens.Start + tokenWords
	return index >= core.Cfg.Value.Region.Tokens.Start && index < hi
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
	n := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	out := make([]uint64, n)

	for i := 0; i < n; i++ {
		idx := core.Cfg.Value.Region.Tokens.Start + i

		if idx >= core.Cfg.Value.Words {
			break
		}

		out[i] = value[idx]
	}

	return out
}

func (value *Value) SetTokenIDs(tokens []uint64) int {
	nWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	n := min(len(tokens), nWords)

	for i := 0; i < n; i++ {
		idx := core.Cfg.Value.Region.Tokens.Start + i

		if idx >= core.Cfg.Value.Words {
			break
		}

		value[idx] = tokens[i]
	}

	return n
}

/*
TokenRegionObservedBytes packs the configured token-region words into one contiguous
little-endian byte serialization (eight bytes per word) and trims trailing zero bytes.

A nil receiver returns nil so absence stays distinguishable from an all-zero region,
which yields a non-nil slice with length zero after trimming.
*/
func (value *Value) TokenRegionObservedBytes() []byte {
	if value == nil {
		return nil
	}

	nWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	base := core.Cfg.Value.Region.Tokens.Start
	buf := make([]byte, 0, nWords*8)

	var wordLE [8]byte

	for wordIdx := 0; wordIdx < nWords; wordIdx++ {
		idx := base + wordIdx

		if idx >= core.Cfg.Value.Words {
			break
		}

		w := value[idx]
		wordLE[0] = byte(w)
		wordLE[1] = byte(w >> 8)
		wordLE[2] = byte(w >> 16)
		wordLE[3] = byte(w >> 24)
		wordLE[4] = byte(w >> 32)
		wordLE[5] = byte(w >> 40)
		wordLE[6] = byte(w >> 48)
		wordLE[7] = byte(w >> 56)

		buf = append(buf, wordLE[:]...)
	}

	for len(buf) > 0 && buf[len(buf)-1] == 0 {
		buf = buf[:len(buf)-1]
	}

	return buf
}

/*
BytesToValue overlays a 1024-byte slice onto the Value pointer via unsafe memory access.
*/
func BytesToValue(p []byte) *Value {
	v := valuePool.Get().(*Value)

	for i := range v {
		v[i] = 0
	}

	if len(p) > core.Cfg.Value.Bytes {
		p = p[:core.Cfg.Value.Bytes]
	}

	if len(p) != core.Cfg.Value.Bytes {
		frame := make([]byte, core.Cfg.Value.Bytes)
		copy(frame, p)
		valueFrom(frame, v)
		return v
	}

	valueFrom(p, v)
	return v
}

/*
ValueToBytes writes the Value's frame (core.Cfg.Value.Bytes bytes) into p.
*/
func ValueToBytes(v *Value, p []byte) error {
	if v == nil {
		return fmt.Errorf("primitive.ValueToBytes: nil value")
	}

	if len(p) < core.Cfg.Value.Bytes {
		return fmt.Errorf(
			"primitive.ValueToBytes: len(p)=%d, need >= %d",
			len(p),
			core.Cfg.Value.Bytes,
		)
	}

	valueTo(v, p)

	return nil
}

func (value *Value) HasProgram() bool {
	// If the firmware register is set, it signals a program should be loaded
	if value[core.Cfg.Value.Region.Registers.FW] > 0 {
		return true
	}

	startWord := core.Cfg.Value.Region.Program.Start
	nProgWords := int((core.Cfg.Value.Region.Program.Bits + 63) / 64)
	endWord := startWord + nProgWords

	if startWord < 0 {
		startWord = 0
	}

	if endWord > core.Cfg.Value.Words {
		endWord = core.Cfg.Value.Words
	}

	for i := startWord; i < endWord; i++ {
		if value[i] != 0 {
			return true
		}
	}

	return false
}
