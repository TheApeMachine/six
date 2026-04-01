//go:generate go run gen.go
package primitive

import (
	"errors"
	"fmt"
	"io"
	"math/bits"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/firmware"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/store"
)

const (
	// Compile-time constants needed for the Value type definition.
	Words    = 128
	ByteSize = Words * 8

	// SignalMask is the mechanical bit-pattern used to reset the sequence index.
	SignalMask = 0xF

	/*
		tokenIDPrime is a 64-bit prime (2^64-59) so TokenID maps
		(multiplier*index + byte) mod prime across the full uint64 range.
	*/
	tokenIDPrime      = uint64(18446744073709551557)
	tokenIDMultiplier = uint64(6364136223846793005)

	// ExecStatusWord is the word reserved for kernel exit markers (high 16 bits);
	// must match generated pkg/compute/kernel/shared/primitives.h EXEC_STATUS_WORD.
	ExecStatusWord  = 63
	ExecStatusShift = 48
)

var tokenIDMulInverse uint64

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

	/*
		stepwiseInstallFn is registered from pkg/compute init so primitive never
		imports stepwise (avoids import cycles with compute/kernel/cpu tests).
	*/
	stepwiseInstallFn func(*Value, core.FirmwareType) bool
)

/*
SetStepwiseInstallFunc registers the handler that attempts to install
programsStepwise.* via pkg/compute/stepwise. Must be called from compute init.
*/
func SetStepwiseInstallFunc(fn func(*Value, core.FirmwareType) bool) {
	stepwiseInstallFn = fn
}

type valueLifecycle struct {
	refs   atomic.Int64
	pooled atomic.Bool
}

type lifecycleShard struct {
	sync.RWMutex
	m map[*Value]*valueLifecycle
}

const numLifecycleShards = 256

var lifecycleShards [numLifecycleShards]lifecycleShard

func init() {
	for i := 0; i < numLifecycleShards; i++ {
		lifecycleShards[i].m = make(map[*Value]*valueLifecycle)
	}

	tokenIDMulInverse = modPowU64(tokenIDMultiplier, tokenIDPrime-2, tokenIDPrime)
}

func getLifecycleShard(value *Value) *lifecycleShard {
	if value == nil {
		return &lifecycleShards[0]
	}

	key := uintptr(unsafe.Pointer(value))
	hash := (key ^ (key >> 16)) % numLifecycleShards
	return &lifecycleShards[hash]
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
type Value [Words]uint64

// CopyFrame overwrites dst with a full-frame copy of src (all Words).
// Kernels may mutate dst while leaving canonical src unchanged (signals on copies; see README).
func CopyFrame(dst, src *Value) {
	if dst == nil || src == nil {
		return
	}
	*dst = *src
}

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
a discarded ValueID. Values should ALWAYS be tombstoned, never just
discarded. You will receive an error from Close if you try to close
a Value that has not been properly tombstoned.
*/
var valuePool = sync.Pool{
	New: func() any {
		val := &Value{}
		val[core.Cfg.Value.Region.ID.Start] = atomic.AddUint64(&globalValueIDCounter, 1)
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

	tokenIDs := make([]uint64, 0, int((core.Cfg.Value.Region.Tokens.Bits+63)/64))

	var seed uint64

	if len(p) > 0 {
		seed = uint64(p[0])
	} else {
		seed = 1
	}

	// LGP introns use legacy 32-bit instruction slots; they corrupt stepwise
	// descriptor bands, so skip when the bootloader comes from programsStepwise.
	useStepwiseBoot := core.Cfg.System.StepwiseUniversalBitwise &&
		core.Cfg.StepwiseFirmwareSource[core.FirmwareTypeBootloader] != ""

	if !useStepwiseBoot {
		// LGP: insert protective introns every 8 instruction slots to prevent
		// catastrophic destruction during Build firmware crossover (IDEAS.md §3).
		firmware.InsertIntrons((*[Words]uint64)(value), 8)
	}

	tokenWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)

	if tokenWords <= 0 {
		return nil, errnie.Error(
			NewValueError(ValueErrorFailedToken),
		)
	}

	// can hold more now since it's superimposed, but let's bound it safely.
	// TODO: Calculate Shannon capacity and do not exceed > 40%.
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

	store.DefaultSpatialIndex().InsertBatch(tokenIDs, *value)

	return value, nil
}

// TokenRegionFingerprint hashes the token region and affinity for indexing (e.g. LSM keys).
func TokenRegionFingerprint(value *Value) uint64 {
	if value == nil {
		return 0
	}
	h := uint64(14695981039346656037)
	nWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	for i := 0; i < nWords; i++ {
		idx := core.Cfg.Value.Region.Tokens.Start + i
		if idx >= Words {
			break
		}
		w := value[idx]
		for shift := 0; shift < 64; shift += 8 {
			h ^= uint64(byte(w >> shift))
			h *= 1099511628211
		}
	}
	h ^= uint64(nWords) + uint64(bits.OnesCount64(value[core.Cfg.Value.Region.Affinity.Start]))
	h *= 1099511628211
	return h
}

// LeftShiftTokens performs a 1-bit circular left shift on the entire Tokens region.
func (value *Value) LeftShiftTokens() {
	nWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	var carry uint64 = 0
	for i := 0; i < nWords; i++ {
		idx := core.Cfg.Value.Region.Tokens.Start + i
		if idx >= Words {
			break
		}
		curr := value[idx]
		nextCarry := curr >> 63
		value[idx] = (curr << 1) | carry
		carry = nextCarry
	}
	// Circular wrap-around
	if carry > 0 {
		idx := core.Cfg.Value.Region.Tokens.Start
		if idx < Words {
			value[idx] |= carry
		}
	}
}

// BindTokenHD XORs the static random 3648-bit FSM signature for the given byte.
func (value *Value) BindTokenHD(b byte) {
	nWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	sig := ByteSignatures[b]
	for i := 0; i < nWords && i < len(sig); i++ {
		idx := core.Cfg.Value.Region.Tokens.Start + i
		if idx >= Words {
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
	if len(p) < ByteSize {
		return 0, io.ErrShortBuffer
	}

	valueTo(value, p)
	return ByteSize, io.EOF
}

/*
Write implements io.Writer as a compatibility shim for the wire format.
We are moving away from the idea of folding Values, as it keeps things
needlessly slower than they could be. From now on Values will only
use their own data for computation, and they will be immutable, so any
computation (which will now be "signals") will emit new Values instead.
*/
func (value *Value) Write(p []byte) (int, error) {
	if len(p) < ByteSize {
		return 0, io.ErrShortBuffer
	}

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
// For untracked Values (created via ViewValue), this is a no-op.
func (value *Value) Release() (err error) {
	lc, ok := lifecycleFor(value)

	if !ok {
		// Untracked value (e.g. ViewValue) — nothing to release.
		return nil
	}

	if err = value.isTombstoned(); err != nil {
		return err
	}

	refs := lc.refs.Add(-1)
	if refs > 0 {
		return nil
	}
	if refs < 0 {
		lc.refs.Store(0)
		return errnie.Error(
			NewValueError(ValueErrorRefcountUnderflow),
		)
	}

	shard := getLifecycleShard(value)
	shard.Lock()
	delete(shard.m, value)
	shard.Unlock()

	if lc.pooled.Load() && (value.shouldRecycleFromExecStatus() || value.isPoolReusable()) {
		value.resetForPool()
		valuePool.Put(value)
	}

	return nil
}

// ViewValue creates a non-pooled, non-lifecycle-tracked copy of a 1024-byte
// frame for read-only inspection (e.g. reading Affinity, calling String()).
// The returned Value is stack-allocated and its Close() is a no-op, so no
// tombstone firmware is needed before discarding it.
func ViewValue(p []byte) *Value {
	var v Value
	if len(p) >= ByteSize {
		valueFrom(p[:ByteSize], &v)
	} else {
		var frame [ByteSize]byte
		copy(frame[:], p)
		valueFrom(frame[:], &v)
	}
	return &v
}

func (value *Value) isTombstoned() error {
	if slices.Max(slices.Concat(
		value[core.Cfg.Value.Region.Tokens.Start:core.Cfg.Value.Region.Tokens.Start+int(core.Cfg.Value.Region.Tokens.Bits/64)-3],
		value[core.Cfg.Value.Region.Affinity.Start:core.Cfg.Value.Region.Affinity.Start+int(core.Cfg.Value.Region.Affinity.Bits/64)],
		value[core.Cfg.Value.Region.Program.Start:core.Cfg.Value.Region.Program.Start+int(core.Cfg.Value.Region.Program.Bits/64)],
	)) > 0 {
		return errnie.Error(NewValueError(ValueErrorNotTombstoned))
	}

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

	checkRegion(core.Cfg.Value.Region.Tokens.Start, core.Cfg.Value.Region.Tokens.Bits)
	checkRegion(core.Cfg.Value.Region.Affinity.Start, core.Cfg.Value.Region.Affinity.Bits)
	checkRegion(core.Cfg.Value.Region.Program.Start, core.Cfg.Value.Region.Program.Bits)

	return wiped
}

// InstallTombstone installs the tombstone firmware into the Value's program
// region and resets the PC. After the next ALU execution the Tokens, Affinity,
// state metadata, and program region are all zeroed in-band via JIT-fused
// ALU FALSE batch — the tombstone is fully self-erasing.
func (value *Value) InstallTombstone() {
	value.installFirmware(core.FirmwareTypeTombstone)
	value[core.Cfg.Value.Region.Registers.PC] = 0
}

// InstallLearnFirmware installs the learn firmware and resets the PC.
func (value *Value) InstallLearnFirmware() {
	value.installFirmware(core.FirmwareTypeLearn)
	value[core.Cfg.Value.Region.Registers.PC] = 0
}

// InstallBuildFirmware installs the build firmware and resets the PC.
func (value *Value) InstallBuildFirmware() {
	value.installFirmware(core.FirmwareTypeBuild)
	value[core.Cfg.Value.Region.Registers.PC] = 0
}

// InstallQueryFirmware installs programs.query from config and resets the PC.
// Host sets fw to core.FirmwareRegisterQuery when routing through the bootloader protocol.
func (value *Value) InstallQueryFirmware() {
	value.installFirmware(core.FirmwareTypeQuery)
	value[core.Cfg.Value.Region.Registers.PC] = 0
}

/*
installFirmware installs the firmware into the Value. This should
ideally only be used to install the bootloader firmware. In all
other cases, setting the fw register to the in-band firmware code and
setting the pc register to the program index (which is where)
the bootloader starts) is the correct way to install firmware.
*/
func (value *Value) installFirmware(fw core.FirmwareType) {
	if stepwiseInstallFn != nil && stepwiseInstallFn(value, fw) {
		return
	}

	prog := core.Cfg.Firmware[fw]
	wordBase := uint64(core.Cfg.Value.Region.Program.Start)

	for i := 0; i < len(prog); i += 4 {
		wordPos := wordBase + uint64(i/4)

		if int(wordPos) >= Words {
			break
		}

		var w uint64
		w = uint64(prog[i])

		if i+1 < len(prog) {
			w |= uint64(prog[i+1]) << 16
		}
		if i+2 < len(prog) {
			w |= uint64(prog[i+2]) << 32
		}
		if i+3 < len(prog) {
			w |= uint64(prog[i+3]) << 48
		}

		value[wordPos] = w
	}
}

/*
String returns the Token region of the Value as a readable string.
It resolves TokenIDs in the LSM by exact Value-frame bitmap match, then
orders by the sequence index packed in each TokenID (Tokenize).
*/
func (value *Value) String() string {
	keys := store.DefaultSpatialIndex().LookupKeysByValue([Words]uint64(*value))
	if len(keys) == 0 {
		return "[superposed state]"
	}

	sort.Slice(keys, func(i, j int) bool {
		_, idxI, okI := DetokenizeTokenID(keys[i])
		_, idxJ, okJ := DetokenizeTokenID(keys[j])
		if !okI || !okJ {
			return keys[i] < keys[j]
		}

		return idxI < idxJ
	})

	var builder strings.Builder
	for _, tid := range keys {
		b, _, ok := DetokenizeTokenID(tid)
		if !ok {
			continue
		}
		if b >= 0x20 && b < 0x7F {
			builder.WriteByte(b)
		}
	}

	if builder.Len() == 0 {
		return "[superposed state]"
	}

	str := builder.String()
	errnie.Info(str)
	return str
}

func (value *Value) Bytes() []byte {
	p := make([]byte, ByteSize)

	if ValueToBytes(value, p) != nil {
		errnie.Error(NewValueError(ValueErrorFailedByteConversion))
		return nil
	}

	return p
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

/*
DetokenizeTokenID recovers (byte, index) from a Tokenize-produced id by
solving (tid - b) ≡ multiplier*index (mod prime) for byte tags in 0..255.
Indices below 2^32 are accepted so arbitrary high residues from wrong
bytes are rejected before the Tokenize confirmation step.
*/
func DetokenizeTokenID(tid uint64) (b byte, index uint64, ok bool) {
	inv := tokenIDMulInverse
	const maxReasonableIndex = uint64(1 << 32)

	for candidate := 0; candidate < 256; candidate++ {
		diff := subModU64(tid, uint64(candidate), tokenIDPrime)
		idx := modMulU64(diff, inv, tokenIDPrime)
		if idx >= maxReasonableIndex {
			continue
		}

		if Tokenize(byte(candidate), idx) == tid {
			return byte(candidate), idx, true
		}
	}

	return 0, 0, false
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
		if idx >= Words {
			break
		}
		out[i] = value[idx]
	}
	return out
}

/*
TokenRegionObservedBytes packs the token region into a byte slice without using
the spatial index. Little-endian byte order per uint64 word; trailing zero bytes
are trimmed. Used by the experiment pipeline after learn for a substrate-faithful
readout that does not depend on exact frame equality in the LSM.
*/
func TokenRegionObservedBytes(value *Value) []byte {
	if value == nil {
		return nil
	}

	tokenBits := core.Cfg.Value.Region.Tokens.Bits
	if tokenBits == 0 {
		return nil
	}

	nWords := int((tokenBits + 63) / 64)
	base := core.Cfg.Value.Region.Tokens.Start
	if nWords <= 0 || base < 0 {
		return nil
	}

	out := make([]byte, 0, nWords*8)

	for wordIdx := 0; wordIdx < nWords; wordIdx++ {
		idx := base + wordIdx
		if idx >= Words {
			break
		}

		word := value[idx]

		for shift := 0; shift < 64; shift += 8 {
			out = append(out, byte(word>>shift))
		}
	}

	for len(out) > 0 && out[len(out)-1] == 0 {
		out = out[:len(out)-1]
	}

	if len(out) == 0 {
		return []byte{}
	}

	return out
}

func (value *Value) SetTokenIDs(tokens []uint64) int {
	nWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	n := min(len(tokens), nWords)
	for i := 0; i < n; i++ {
		idx := core.Cfg.Value.Region.Tokens.Start + i
		if idx >= Words {
			break
		}
		value[idx] = tokens[i]
	}
	return n
}

func registerPooledLifecycle(value *Value) {
	if value == nil {
		return
	}

	shard := getLifecycleShard(value)

	shard.Lock()
	entry, ok := shard.m[value]
	if !ok {
		entry = &valueLifecycle{}
		shard.m[value] = entry
	}
	shard.Unlock()

	entry.pooled.Store(true)
	entry.refs.Store(1)
}

func lifecycleFor(value *Value) (*valueLifecycle, bool) {
	if value == nil {
		return nil, false
	}

	shard := getLifecycleShard(value)

	shard.RLock()
	entry, ok := shard.m[value]
	shard.RUnlock()

	return entry, ok
}

/*
Clone is used to create a new Value from an existing one, which is how
we emit new Values from the substrate. It copies the entire frame, including
the ValueID, so we need to set a new ValueID for the clone. We intentionally
do not install the bootloader, because it should already be there.
*/
func (value *Value) Clone() *Value {
	clone := valuePool.Get().(*Value)
	for i := range value {
		clone[i] = value[i]
	}
	registerPooledLifecycle(clone)
	clone[core.Cfg.Value.Region.ID.Start] = atomic.AddUint64(&globalValueIDCounter, 1)
	// Do not blindly install bootloader on a clone, we want to clone the exact state!
	return clone
}

/*
BytesToValue overlays a 1024-byte slice onto the Value pointer via unsafe memory access.
*/
func BytesToValue(p []byte) *Value {
	v := valuePool.Get().(*Value)
	registerPooledLifecycle(v)
	for i := range v {
		v[i] = 0
	}
	if len(p) > ByteSize {
		p = p[:ByteSize]
	}
	if len(p) != ByteSize {
		var frame [ByteSize]byte
		copy(frame[:], p)
		valueFrom(frame[:], v)
		return v
	}
	valueFrom(p, v)

	return v
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
	ValueErrorFailedToken          ValueErrorType = "failed_token"
	ValueErrorDivergence           ValueErrorType = "divergence"
	ValueErrorDataFull             ValueErrorType = "data_full"
	ValueErrorInvalidProgramWord   ValueErrorType = "invalid_program_word"
	ValueErrorRefcountUnderflow    ValueErrorType = "refcount_underflow"
	ValueErrorFailedByteConversion ValueErrorType = "failed_byte_conversion"
	ValueErrorNotTombstoned        ValueErrorType = "not_tombstoned"
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
