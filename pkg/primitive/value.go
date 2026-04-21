//go:generate go run gen.go
package primitive

import (
	"errors"
	"io"
	"log"
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

	signalsStart, signalsWords = SignalsRegion.WordExtent()
	_, contextWords            = ContextRegion.WordExtent()
	_, gradientWords           = GradientRegion.WordExtent()
	_, propertiesWords         = PropertiesRegion.WordExtent()

	stageWords    = signalsWords + contextWords + gradientWords + propertiesWords
	assetStart, _ = AssetRegion.WordExtent()
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

If geometry.SlotCode(rollingDigest, rawByte, positionOrdinal) collides with an
occupied 16-bit key, newValuesFromPayload advances through higher ordinals
until a free key appears so duplicate stream bytes are preserved in additional
slot pairs rather than being dropped; tokenBytes and offset still govern how
many pairs fit in buf before chaining another segment.

To load an exact wire frame without re-stamping (same ID and affinity bits
as Read produced), use Write on a pooled *Value — never a second NewValue on
that frame.

The returned slice is never empty on success. Each element is owned by the
caller until CloseAll or Close returns them to the arena (or heap fallback).
Segments are linked at mint time: word Prev on segment i+1 holds segment
i's ID; word Next on segment i holds segment i+1's ID (config
value.region.prev / next). The first segment's Prev and the last segment's
Next remain zero so consumers can detect chain endpoints. There is no
truncation: when the Morton slab fills, packing continues in a fresh segment.

A bootstrap affinity fingerprint is also folded in at mint: the populated
token words are XOR-rolled into the configured affinity region (one word per
affinity slot, modulo the affinity word count). This gives the routing parent
real signal on first contact even before the `affinity` firmware program has
had a chance to run, while remaining bit-identical to what that program
would compute on a single pass.

CloseAll closes every non-nil pointer in the slice.
*/
func NewValue(p []byte, labels ...uint64) ([]*Value, error) {
	if len(p) == 0 {
		return nil, errors.Join(
			io.ErrShortBuffer,
			errors.New("newValue: len(p) == 0"),
		)
	}

	tokenWords := int(core.Cfg.Value.Region.Tokens.Bits / 64)
	maxSegCodes := tokenWords * 4
	out := make([]*Value, 0, (len(p)+maxSegCodes-1)/maxSegCodes)

	var label uint64

	if len(labels) > 0 {
		for _, l := range labels {
			if l > 0 {
				label = l
				break
			}
		}
	}

	prevStart := core.Cfg.Value.Region.Prev.Start
	nextStart := core.Cfg.Value.Region.Next.Start
	affinityStart, affinityWords := AffinityRegion.WordExtent()

	for idx := 0; idx < len(p); {
		val := AllocValue()

		// Initialize SURPRISAL to a high value so it doesn't immediately resolve
		val.Set(core.Cfg.Value.Region.Properties.Start+int(SURPRISAL), 512)

		codes := tokenSlabWords(val)
		maxCodes := len(codes) * 4
		n, pos := 0, uint32(0)

		for idx < len(p) && n < maxCodes {
			// Match geometry.SlotCode and Value.String: the raw byte lives on the Y
			// axis of EncodeInterleaved8x8 so DecodeInterleaved8x8’s Y recovers it.
			code := EncodeInterleaved8x8(pos, uint32(p[idx]))
			idx++
			pos++

			if n > 0 {
				duplicate := false

				for i := 0; i < n; i++ {
					if mortonCodeAt(codes, i) == code {
						duplicate = true

						break
					}
				}

				if duplicate {
					continue
				}
			}

			setMortonCodeAt(codes, n, code)
			n++
		}

		if n == 0 {
			FreeValue(val)

			for _, x := range out {
				FreeValue(x)
			}

			return nil, errors.Join(
				io.ErrShortBuffer,
				errors.New("newValue: n == 0"),
			)
		}

		stamp := val.stampID()

		if label > 0 {
			stamp.Set(
				core.Cfg.Value.Region.Properties.Start,
				label,
			)
		}

		// Bootstrap affinity: XOR-fold the populated token words into the
		// affinity slots, distributing across the available affinity words
		// modulo affinityWords. Equivalent to the first pass of the
		// `affinity` firmware program.
		if affinityWords > 0 {
			for i := range tokenWords {
				w := codes[i]

				if w == 0 {
					continue
				}

				(*stamp)[affinityStart+(i%affinityWords)] ^= w
			}
		}

		// Stamp prev/next links across adjacent segments. The previous
		// segment learns this segment's ID as its Next; this segment
		// learns the previous segment's ID as its Prev. Heads and tails
		// stay zero so consumers can detect chain endpoints.
		if len(out) > 0 {
			previous := out[len(out)-1]
			previous.Set(nextStart, stamp.ID())
			stamp.Set(prevStart, previous.ID())
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
		return nil, errors.Join(
			io.ErrShortBuffer,
			errors.New("valueFromWireFrame: len(frame) < core.Cfg.Value.Bytes"),
		)
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
		return errors.Join(
			io.ErrShortBuffer,
			errors.New("value.LoadFullFrame: len(frame) < core.Cfg.Value.Bytes"),
		)
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
		return 0, errors.Join(
			io.ErrShortBuffer,
			errors.New("value.Write: len(p) < core.Cfg.Value.Bytes"),
		)
	}

	valueTo(value, p)
	return core.Cfg.Value.Bytes, io.EOF
}

/*
Write receives an incoming Value as bytes and stages the visiting frame into
the host's Asset region.

Staging is uniform: the inbound Signals + Context + Gradient + Properties
block lands at asset[0..stageWords). That is the only behaviour Write has —
there is no special Go-side path that copies a peer's program region across
based on the visitor's role. Programmer Values express install in-band by
carrying their compiled bytecode in their Tokens region (the substrate's
"tape") and emitting it through their own program (the bytecode is moved
into the visitor's Signals before encounter, so it lands in the host's
asset[0..8). A resident bootstrap program on the host then OR-accumulates
that staging slot back into program[0..8), overwriting the bootstrap with
the new firmware on the next ALU pass.
*/
func (value *Value) Write(p []byte) (int, error) {
	if len(p) < core.Cfg.Value.Bytes {
		return 0, errors.Join(
			io.ErrShortBuffer,
			errors.New("value.Write: len(p) < core.Cfg.Value.Bytes"),
		)
	}

	tmpVal := AllocValue()
	valueFrom(p, tmpVal)

	copy(
		(*value)[assetStart:assetStart+stageWords],
		(*tmpVal)[signalsStart:signalsStart+stageWords],
	)

	status, err := tmpVal.Property(STATUS)
	if err != nil {
		FreeValue(tmpVal)
		return 0, err
	}
	value.Set(core.Cfg.Value.Region.Properties.Start+int(STATUS), status)

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
to region length). The bytes are exactly what the universal-bitwise kernel
will execute next pass; callers that also have a continuation word should
follow up with SetSchedulingNext so the install buffer is complete before
the Value runs.
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
SchedulingNextProgramWord is the absolute word index inside the asset region
that the runtime checks for `next <id>` / `next self` continuations. Mirrors
pkg/compute/kernel.SchedulingNextProgramWord (kept here to avoid an import
cycle from primitive into kernel).
*/
const SchedulingNextProgramWord = 117

/*
SetSchedulingNext stamps the continuation word that drives `next self` and
`next <id>` semantics. Pass 0 to clear; pass the resident Value's ID for a
self-loop (callers usually go through ProgramConfig.ResolveSchedulingNext
which does that substitution against the SelfSentinel).
*/
func (value *Value) SetSchedulingNext(next uint64) {
	if value == nil {
		return
	}

	value.Set(SchedulingNextProgramWord, next)
}

/*
SchedulingNext returns word 117: continuation for `next <id>` / `next self` (0 = none).
*/
func (value *Value) SchedulingNext() uint64 {
	if value == nil {
		return 0
	}

	w := SchedulingNextProgramWord

	if w < 0 || w >= len(*value) {
		return 0
	}

	return (*value)[w]
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
tokenSlabWords returns the full configured tokens region as a mutable []uint64
slice. Unlike TokenWords, it is not trimmed: callers building a slab from
scratch (NewValue) need every word addressable while the region is still zero.
*/
func tokenSlabWords(value *Value) []uint64 {
	tokenStart := core.Cfg.Value.Region.Tokens.Start
	tokenWords := int(core.Cfg.Value.Region.Tokens.Bits / 64)

	return (*value)[tokenStart : tokenStart+tokenWords]
}

func mortonCodeAt(codes []uint64, idx int) uint16 {
	return uint16(codes[idx/4] >> ((idx % 4) * 16))
}

func setMortonCodeAt(codes []uint64, idx int, code uint16) {
	word := idx / 4
	shift := (idx % 4) * 16
	mask := uint64(0xFFFF) << shift

	codes[word] = (codes[word] &^ mask) | (uint64(code) << shift)
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
