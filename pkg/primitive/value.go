//go:generate go run gen.go
package primitive

import (
	"io"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/core"
)

const valueScratchCap = 1024

var (
	valueTo   func(*Value, []byte)
	valueFrom func([]byte, *Value)
)

/*
Value is a programmable type that acts as a token for
machine intelligence.

Governed by the following rules:

- Value computes on itself
- Value encounters alter computation
*/
type Value [128]uint64

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

var valueIDSeq atomic.Uint64

/*
NewValue requires a non-empty payload. valueFrom copies it into the Value
layout, ComputeAffinityLSH fills affinity from the token region, and stampID
assigns a new ID. The result is ready for backend execution, Publish, and
trie storage.

To load an exact wire frame without re-stamping (same ID and affinity bits
as Read produced), use Write on a pooled *Value — never a second NewValue on
that frame.

The returned pointer is owned by the caller until Close returns it to
valuePool.
*/
func NewValue(p []byte) (*Value, error) {
	if len(p) == 0 {
		return nil, io.ErrShortBuffer
	}

	raw := valuePool.Get()
	value := raw.(*Value)
	valueFrom(p, value)

	if err := value.ComputeAffinityLSH(); err != nil {
		*value = Value{}
		valuePool.Put(value)

		return nil, err
	}

	return value.stampID(), nil
}

/*
ValueFromWireFrame restores a Value from a full Value.Bytes frame produced by
Value.Read. ID, affinity, and every word match the frame; nothing is
recomputed or re-stamped.

The caller owns the returned pointer until Close returns it to valuePool.
*/
func ValueFromWireFrame(frame []byte) (*Value, error) {
	if len(frame) < core.Cfg.Value.Bytes {
		return nil, io.ErrShortBuffer
	}

	raw := valuePool.Get()
	value := raw.(*Value)

	if _, err := value.Write(frame); err != nil {
		*value = Value{}
		valuePool.Put(value)

		return nil, err
	}

	return value, nil
}

func (value *Value) stampID() *Value {
	if value == nil {
		return nil
	}

	value.Set(core.Cfg.Value.Region.ID.Start, valueIDSeq.Add(1))
	return value
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
Write decodes a full Value.Bytes wire frame into an existing Value. It does
not recompute affinity or assign an ID; those words come from the frame.
Use NewValue to mint from payload bytes with fresh ID and LSH-derived affinity
for Publish and trie routing.
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
and returns the value to the value pool.
*/
func (value *Value) Close() error {
	if value == nil {
		return nil
	}

	// Wipe the Value, this is important to ensure
	// that the Value is not leaked to the heap.
	*value = Value{}
	valuePool.Put(value)

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
ID returns the ID of the Value.
*/
func (value *Value) ID() uint64 {
	if value == nil {
		return 0
	}

	return (*value)[core.Cfg.Value.Region.ID.Start]
}

/*
AffinityVector returns the 257-bit affinity region as a fixed-size array.
The last word is masked to AffinityLastWordMask so only bit 0 survives.
*/
func (value *Value) AffinityVector() [AffinityWords]uint64 {
	var aff [AffinityWords]uint64

	if value == nil {
		return aff
	}

	start := core.Cfg.Value.Region.Affinity.Start

	for wordIdx := range AffinityWords {
		idx := start + wordIdx

		if idx < 0 || idx >= len(*value) {
			break
		}

		aff[wordIdx] = (*value)[idx]
	}

	aff[AffinityWords-1] &= AffinityLastWordMask

	return aff
}

/*
SetAffinityVector writes the full affinity region, masking the last word.
*/
func (value *Value) SetAffinityVector(aff [AffinityWords]uint64) {
	if value == nil {
		return
	}

	start := core.Cfg.Value.Region.Affinity.Start

	for wordIdx := range AffinityWords {
		idx := start + wordIdx

		if idx >= len(*value) {
			break
		}

		w := aff[wordIdx]

		if wordIdx == AffinityWords-1 {
			w &= AffinityLastWordMask
		}

		(*value)[idx] = w
	}
}

/*
ContextVector returns the 512-bit context region. This region holds
XOR-bound variable bindings — the compositional context that enables
do-calculus operations via bind/unbind.
*/
func (value *Value) ContextVector() [RegionWords]uint64 {
	var ctx [RegionWords]uint64

	if value == nil {
		return ctx
	}

	start := core.Cfg.Value.Region.Context.Start

	for wordIdx := range RegionWords {
		idx := start + wordIdx

		if idx >= len(*value) {
			break
		}

		ctx[wordIdx] = (*value)[idx]
	}

	return ctx
}

/*
SetContextVector writes the full context region.
*/
func (value *Value) SetContextVector(ctx [RegionWords]uint64) {
	if value == nil {
		return
	}

	start := core.Cfg.Value.Region.Context.Start

	for wordIdx := range RegionWords {
		idx := start + wordIdx

		if idx >= len(*value) {
			break
		}

		(*value)[idx] = ctx[wordIdx]
	}
}

/*
BindContext XORs the given affinity vector into the first AffinityWords
of the context region, creating a compositional binding. XOR is its own
inverse: binding twice with the same vector unbinds it. This is the
substrate for Pearl's do-operator — severing a variable from its causal
parents is an unbind, forcing a new value is a rebind. The remaining
context words (AffinityWords..RegionWords-1) are untouched, available
for non-affinity bindings.
*/
func (value *Value) BindContext(binding [AffinityWords]uint64) {
	if value == nil {
		return
	}

	start := core.Cfg.Value.Region.Context.Start

	for wordIdx := range AffinityWords {
		idx := start + wordIdx

		if idx >= len(*value) {
			break
		}

		(*value)[idx] ^= binding[wordIdx]
	}
}

/*
GradientVector returns the 512-bit gradient region. Under causal
framing this tracks intervention residual — the difference between
predicted and observed outcomes when a variable was intervened on.
Accumulated over multiple interventions, this is the noise term
in a structural causal model.
*/
func (value *Value) GradientVector() [RegionWords]uint64 {
	var grad [RegionWords]uint64

	if value == nil {
		return grad
	}

	start := core.Cfg.Value.Region.Gradient.Start

	for wordIdx := range RegionWords {
		idx := start + wordIdx

		if idx >= len(*value) {
			break
		}

		grad[wordIdx] = (*value)[idx]
	}

	return grad
}

/*
AccumulateGradient XORs a residual vector into the gradient region,
building up the latent noise term from repeated interventions.
*/
func (value *Value) AccumulateGradient(residual [RegionWords]uint64) {
	if value == nil {
		return
	}

	start := core.Cfg.Value.Region.Gradient.Start

	for wordIdx := range RegionWords {
		idx := start + wordIdx

		if idx >= len(*value) {
			break
		}

		(*value)[idx] ^= residual[wordIdx]
	}
}

/*
MetaWord offsets within the 512-bit meta region.
*/
const (
	MetaConfidence        = 0
	MetaNovelty           = 1
	MetaStability         = 2
	MetaUseCount          = 3
	MetaCreatedEpoch      = 4
	MetaLastAccessEpoch   = 5
	MetaInterventionCount = 6
	MetaFlags             = 7
)

/*
MetaWord reads a single word from the meta region at the given offset
(0–7). Returns 0 for nil Values or out-of-range offsets.
*/
func (value *Value) MetaWord(offset int) uint64 {
	if value == nil || offset < 0 || offset >= RegionWords {
		return 0
	}

	idx := core.Cfg.Value.Region.Meta.Start + offset

	if idx >= len(*value) {
		return 0
	}

	return (*value)[idx]
}

/*
SetMetaWord writes a single word into the meta region at the given offset.
*/
func (value *Value) SetMetaWord(offset int, val uint64) {
	if value == nil || offset < 0 || offset >= RegionWords {
		return
	}

	idx := core.Cfg.Value.Region.Meta.Start + offset

	if idx >= len(*value) {
		return
	}

	(*value)[idx] = val
}

/*
IncrementMeta performs a non-atomic read-modify-write on a meta word
via (*value)[idx]++. Suitable for use-count and intervention-count
tracking; callers must serialize through a trie's update path — there
is no atomic or lock-free guarantee on the underlying store word.
*/
func (value *Value) IncrementMeta(offset int) {
	if value == nil || offset < 0 || offset >= RegionWords {
		return
	}

	idx := core.Cfg.Value.Region.Meta.Start + offset

	if idx >= len(*value) {
		return
	}

	(*value)[idx]++
}

/*
TokenRegionBytes returns the token slab bytes with trailing NUL padding trimmed.
It matches what String exposes as UTF-8 data without allocating an intermediate
string — safe only for immediate hashing or read-only use; do not retain the
slice across mutations to the Value words.
*/
func (value *Value) TokenRegionBytes() []byte {
	if value == nil {
		return nil
	}

	tokenByteLen := int((core.Cfg.Value.Region.Tokens.Bits + 7) / 8)
	startByte := core.Cfg.Value.Region.Tokens.Start * 8

	slab := unsafe.Slice(
		(*byte)(unsafe.Pointer(&value[0])),
		startByte+tokenByteLen,
	)[startByte:]

	trim := len(slab)

	for trim > 0 && slab[trim-1] == 0 {
		trim--
	}

	return slab[:trim]
}

/*
String returns the string representation of the
Value's token region, which stores the original
bytes of the data that was used to create the Value.
*/
func (value *Value) String() string {
	return string(value.TokenRegionBytes())
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
