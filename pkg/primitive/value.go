//go:generate go run gen.go
package primitive

import (
	"io"
	"log"
	"slices"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
)

/*
RegionType names coarse Value regions for later lowering from region refs.

Source text uses string refs (e.g. tokens[0,16]); lowering maps those into
the packed Value layout via RegionRef.
*/
type RegionType uint8

const (
	TokenRegion RegionType = iota
	ProgramRegion
	SignalsRegion
	ContextRegion
	GradientRegion
	PropertiesRegion
	AssetRegion
	PrevRegion
	NextRegion
	IDRegion
	AffinityRegion
)

var (
	valueTo     func(*Value, []byte)
	valueFrom   func([]byte, *Value)
	RegionNames = map[string]RegionType{
		"tokens":     TokenRegion,
		"program":    ProgramRegion,
		"signals":    SignalsRegion,
		"context":    ContextRegion,
		"gradient":   GradientRegion,
		"properties": PropertiesRegion,
		"asset":      AssetRegion,
		"prev":       PrevRegion,
		"next":       NextRegion,
		"id":         IDRegion,
		"affinity":   AffinityRegion,
	}

	signalsStart, signalsWords       = SignalsRegion.WordExtent()
	contextStart, contextWords       = ContextRegion.WordExtent()
	gradientStart, gradientWords     = GradientRegion.WordExtent()
	propertiesStart, propertiesWords = PropertiesRegion.WordExtent()

	stageWords             = signalsWords + contextWords + gradientWords + propertiesWords
	assetStart, assetWords = AssetRegion.WordExtent()
)

/*
Value is a programmable type that acts as a token for
machine intelligence.

Governed by the following rules:

- Value computes on itself
- Value encounters alter computation
*/
type Value [128]uint64

/*
FrameMultivector is the primitive-level 512-bit geometric payload. It mirrors
the PGA even subalgebra layout used by pkg/core/numeric/geometry without
importing that package, which would create a package cycle.
*/
type FrameMultivector [8]float64

func init() {
	x := uint16(1)

	// Fast-path memory alignment for little-endian architectures
	if *(*byte)(unsafe.Pointer(&x)) == 1 {
		valueTo = func(v *Value, p []byte) {
			copy(p, unsafe.Slice(
				(*byte)(unsafe.Pointer(&v[0])),
				core.Cfg.Value.Bytes,
			))
		}
		valueFrom = func(p []byte, v *Value) {
			copy(unsafe.Slice(
				(*byte)(unsafe.Pointer(&v[0])),
				core.Cfg.Value.Bytes,
			), p)
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

var valueIDSeq atomic.Uint64

/*
NewValue requires a non-empty payload. Optional variadic label slices are
supervision metadata: the first non-empty label stamps PropertiesStartWord via
kernel.LabelPropertiesWord (later non-empty entries are ignored so the word
stays a single fingerprint). Call NewValue(p) with no extra args when no label
applies. The Morton token slab is written into
the configured tokens region, the full wire frame is materialized, and stampID
assigns a new ID. The result is ready for backend execution, Publish, and
trie storage.

Morton slots are filled in input order until the token slab cannot hold
another 16-bit code. The slab byte length comes from value.region.tokens.bits
(default 1024 bits → 128 B, i.e. up to sixty-four 16-bit codes). Each slot
pairs the payload byte with a geometry-derived Morton position code (see
geometry.SlotCode, geometry.PositionCode, and the per-segment positionOrdinal
and idx scan in newValuesFromPayload). When no geometry is provided, NewValue
uses a balanced default lattice sized to the segment capacity.

If geometry.SlotCode(rollingDigest, rawByte, positionOrdinal) collides with an occupied 16-bit
key, newValuesFromPayload advances through higher ordinals until a free key
appears so duplicate stream bytes are preserved in additional slot pairs
rather than being dropped; tokenBytes and offset still govern how many pairs
fit in buf before chaining another segment.

To load an exact wire frame without re-stamping (same ID and affinity bits
as Read produced), use Write on a pooled *Value — never a second NewValue on
that frame.

The returned slice is never empty on success. Each element is owned by the
caller until CloseAll or Close returns them to the arena (or heap fallback).
Segments are linked:
word Prev on segment i+1 holds segment i's ID; word Next on segment i holds
segment i+1's ID (config value.region.prev / next). There is no truncation:
when the Morton slab fills, packing continues in a fresh segment.

CloseAll closes every non-nil pointer in the slice.
*/
func NewValue(p []byte, labels ...[]byte) ([]*Value, error) {
	if len(p) == 0 {
		return nil, io.ErrShortBuffer
	}

	value := AllocValue()
	maxCodes := int(core.Cfg.Value.Region.Tokens.Bits / 16)
	out := make([]*Value, 0, maxCodes)

	var label uint64

	labelsPtr := unsafe.Slice((*uint64)(unsafe.Pointer(&labels[0])), len(labels))

	if i := slices.IndexFunc(labelsPtr, func(c uint64) bool { return c > 0 }); i >= 0 {
		label = labelsPtr[i]
	}

	for idx := 0; idx < len(p); {
		val := AllocValue()
		codes := value.TokenWords()
		n, pos := 0, uint32(0)

		for idx < len(p) && n < maxCodes {
			code := EncodeInterleaved8x8(uint32(p[idx]), pos)
			idx++
			pos++

			if slices.Contains(unsafe.Slice(
				(*uint16)(unsafe.Pointer(&codes[0])),
				n,
			), code) {
				continue
			}

			codes[n] = uint64(code)
			n++
		}

		if n == 0 {
			FreeValue(val)

			for _, x := range out {
				FreeValue(x)
			}

			return nil, io.ErrShortBuffer
		}

		stamp := val.stampID()

		if label > 0 {
			stamp.Set(
				core.Cfg.Value.Region.Properties.Start,
				label,
			)
		}

		out = append(out, stamp)
	}
	return out, nil
}

/*
CloseAll returns every Value in the slice to the arena. Nil entries are skipped.
*/
func CloseAll(values []*Value) {
	for _, value := range values {
		if value == nil {
			continue
		}

		if err := value.Close(); err != nil {
			log.Println("error closing value", err)
		}
	}
}

/*
ValueFromWireFrame restores a Value from a full Value.Bytes frame produced by
Value.Read. ID, affinity, and every word match the frame; nothing is
recomputed or re-stamped.

The caller owns the returned pointer until Close returns it to the arena.
*/
func ValueFromWireFrame(frame []byte) (*Value, error) {
	if len(frame) < core.Cfg.Value.Bytes {
		return nil, io.ErrShortBuffer
	}

	value := AllocValue()
	valueFrom(frame, value)

	return value, nil
}

/*
LoadFullFrame replaces every word in this Value from a complete wire frame
of length core.Cfg.Value.Bytes (the layout produced by Value.Read). This is
the in-place analogue of ValueFromWireFrame without a second allocation.
*/
func (value *Value) LoadFullFrame(frame []byte) error {
	if value == nil {
		return io.ErrClosedPipe
	}

	if len(frame) < core.Cfg.Value.Bytes {
		return io.ErrShortBuffer
	}

	valueFrom(frame, value)

	return nil
}

func (value *Value) stampID() *Value {
	if value == nil {
		return nil
	}

	value.Set(core.Cfg.Value.Region.ID.Start, valueIDSeq.Add(1))

	return value
}

/*
StampNewID assigns the next sequential ID word for Values minted outside NewValue
(unsupervised learners, probes, and other in-place constructions that still need
a trie-routable identity).
*/
func (value *Value) StampNewID() *Value {
	return value.stampID()
}

/*
Read implements io.Reader, which prepares the Value for
transmission over the wire.
It is important to understand that we do not pay any
traditional serialization tax, because the Value is already
serialized in memory.

A successful read of the full frame returns (Bytes, io.EOF) as a
single-shot delimiter — stream assemblers that keep pulling frames
(vm.Tokenizer, etc.) must treat a full read as success, not as
end of the byte source.
*/
func (value *Value) Read(p []byte) (int, error) {
	if len(p) < core.Cfg.Value.Bytes {
		return 0, io.ErrShortBuffer
	}

	valueTo(value, p)
	return core.Cfg.Value.Bytes, io.EOF
}

/*
Write receives an incoming Value as bytes and materializes it
into a temporary Value, so we can copy the Signals, Context,
Gradient, and Properties into the Assets region of the host.
*/
func (value *Value) Write(p []byte) (int, error) {
	if len(p) < core.Cfg.Value.Bytes {
		return 0, io.ErrShortBuffer
	}

	tmpVal := AllocValue()
	valueFrom(p, tmpVal)

	roleWord, errRole := tmpVal.Property(ROLE)
	target, errTarget := tmpVal.Property(TARGET)

	if errRole == nil && errTarget == nil &&
		roleWord == uint64(ValueRoleProgrammer) && target != 0 {
		progStart, progN := core.Cfg.Value.Region.Program.WordExtent()

		if progN > 0 {
			copy(
				(*value)[progStart:progStart+progN],
				(*tmpVal)[progStart:progStart+progN],
			)
		}
	}

	copy(
		(*value)[assetStart:assetStart+stageWords],
		(*tmpVal)[signalsStart:signalsStart+stageWords],
	)

	FreeValue(tmpVal)
	return core.Cfg.Value.Bytes, nil
}

/*
Close implements io.Closer, and must be called when a Value
is discarded. It guarantees a sane exit from the substrate
and returns the value to the value pool.
*/
func (value *Value) Close() error {
	if value == nil {
		return nil
	}

	// Wipe the Value, this is important to ensure
	// that the Value is not leaked to the heap.
	*value = Value{}
	FreeValue(value)

	return nil
}

/*
Set sets the value of the Value.
*/
func (value *Value) Set(region int, data uint64) {
	if value == nil {
		return
	}

	if region < 0 || region >= len(*value) {
		return
	}

	(*value)[region] = data
}

/*
WriteProgramWords copies words into the configured program region (clamped
to region length). Used with core.NamedProgramWords after config load.
*/
func (value *Value) WriteProgramWords(words []uint64) {
	if value == nil {
		return
	}

	start, n := core.Cfg.Value.Region.Program.WordExtent()

	if n <= 0 {
		return
	}

	for i := 0; i < n && i < len(words); i++ {
		value.Set(start+i, words[i])
	}
}

/*
WordExtent returns the absolute start word index and word count for this
coarse region name, matching Value.Get slicing for the active config.
*/
func (region RegionType) WordExtent() (start int, words int) {
	r := core.Cfg.Value.Region

	switch region {
	case TokenRegion:
		return r.Tokens.WordExtent()
	case ProgramRegion:
		return r.Program.WordExtent()
	case SignalsRegion:
		return r.Signals.WordExtent()
	case ContextRegion:
		return r.Context.WordExtent()
	case GradientRegion:
		return r.Gradient.WordExtent()
	case PropertiesRegion:
		return r.Properties.WordExtent()
	case AssetRegion:
		return r.Asset.WordExtent()
	case PrevRegion:
		return r.Prev.WordExtent()
	case NextRegion:
		return r.Next.WordExtent()
	case IDRegion:
		return r.ID.WordExtent()
	case AffinityRegion:
		return r.Affinity.WordExtent()
	}

	return 0, 0
}

/*
Get returns the uint64 words backing the given coarse region.
*/
func (value *Value) Get(region RegionType) []uint64 {
	if value == nil {
		return nil
	}

	start, words := region.WordExtent()

	if words == 0 {
		return nil
	}

	end := start + words

	if start < 0 || end > len(*value) || start > end {
		return nil
	}

	return (*value)[start:end]
}

/*
ID returns the ID of the Value.
*/
func (value *Value) ID() uint64 {
	if value == nil {
		return 0
	}

	return (*value)[core.Cfg.Value.Region.ID.Start]
}

/*
TokenWords returns the populated token words from the token region,
trimming trailing zero words.
*/
func (value *Value) TokenWords() []uint64 {
	if value == nil {
		return nil
	}

	tokenStart := core.Cfg.Value.Region.Tokens.Start
	tokenWords := int(core.Cfg.Value.Region.Tokens.Bits / 64)

	slab := (*value)[tokenStart : tokenStart+tokenWords]

	trim := len(slab)
	for trim > 0 && slab[trim-1] == 0 {
		trim--
	}

	return slab[:trim]
}

/*
String decodes the Morton-coded token region back to the original
byte sequence for human-readable output. All algorithmic paths should
operate on the raw Morton-coded TokenWords directly.
*/
func (value *Value) String() string {
	if value == nil {
		return ""
	}

	words := value.TokenWords()
	if len(words) == 0 {
		return ""
	}

	out := make([]byte, 0, len(words)*4)

	for _, word := range words {
		for i := 0; i < 4; i++ {
			code := uint16(word >> (i * 16))
			if code == 0 {
				continue
			}

			_, rawByte := DecodeInterleaved8x8(code)
			out = append(out, byte(rawByte))
		}
	}

	return string(out)
}

/*
Bytes returns the bytes of the Value.
*/
func (value *Value) Bytes() []byte {
	return unsafe.Slice(
		(*byte)(unsafe.Pointer(&value[0])),
		core.Cfg.Value.Bytes,
	)
}
