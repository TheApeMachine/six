package data

import (
	"context"
	"fmt"
	"math/bits"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/system/console"
	config "github.com/theapemachine/six/pkg/system/core"
	"github.com/theapemachine/six/pkg/system/pool"
)

const (
	affineFieldMask          = uint64(0x1FF)
	affineWordShiftScale     = 0
	affineWordShiftTranslate = 9
	affineWordMaskScale      = affineFieldMask << affineWordShiftScale
	affineWordMaskTranslate  = affineFieldMask << affineWordShiftTranslate

	shellWordShiftTrajectoryFrom = 18
	shellWordShiftTrajectoryTo   = 27
	shellWordShiftGuardRadius    = 36
	shellWordShiftRouteHint      = 44
	shellWordShiftFlags          = 52

	shellWordMaskTrajectoryFrom = affineFieldMask << shellWordShiftTrajectoryFrom
	shellWordMaskTrajectoryTo   = affineFieldMask << shellWordShiftTrajectoryTo
	shellWordMaskGuardRadius    = uint64(0xFF) << shellWordShiftGuardRadius
	shellWordMaskRouteHint      = uint64(0xFF) << shellWordShiftRouteHint
	shellWordMaskFlags          = uint64(0xFFF) << shellWordShiftFlags
)

const (
	ValueFlagTrajectory uint16 = 1 << iota
	ValueFlagRouteHint
	ValueFlagGuard
	ValueFlagMutable
)

var (
	/*
		canonicalValueBasis is a 5-mark Golomb ruler projected into GF(257).
		The pairwise deltas are unique, giving each lexical value a sparse
		resonant fingerprint rather than a handful of arbitrary coprime hits.
	*/
	canonicalValueBasis = [5]int{0, 1, 4, 9, 11}

	/*
		byteValueMultipliers stores 3^(byte+1) mod 257 for every byte.
		Affine scaling by a primitive-root orbit preserves the sparse shape
		while rotating it through the Fermat field.
	*/
	byteValueMultipliers = initByteValueMultipliers()

	bytePhaseScales = initBytePhaseScales()
	byteRouteHints  = initByteRouteHints()
)

func initBytePhaseScales() [256]numeric.Phase {
	var scales [256]numeric.Phase
	for b := 0; b < len(scales); b++ {
		scales[b] = numeric.Phase(modPow257(int(numeric.FermatPrimitive), b))
		if scales[b] == 0 {
			scales[b] = 1
		}
	}
	return scales
}

func initByteRouteHints() [256]uint8 {
	var hints [256]uint8
	for b := 0; b < len(hints); b++ {
		base := BaseValue(byte(b))
		hints[b] = uint8(ValueBin(&base))
	}
	return hints
}

func normalizePhaseWord(phase numeric.Phase) uint64 {
	return uint64(uint32(phase) % numeric.FermatPrime)
}

/*
PhaseScaleForByte returns the multiplicative GF(257) transition induced by a
byte. It is the native phase operator behind the old lexical update rule
state' = state * 3^byte (mod 257), but factored out so stored values can carry
that operator directly without redundantly storing the byte seed.
*/
func PhaseScaleForByte(symbol byte) numeric.Phase {
	return bytePhaseScales[int(symbol)]
}

/*
RouteHintForSymbol returns a compact routing class for a byte without storing
the byte itself in the persistent value. It is derived from the byte's sparse
lexical seed and only used as an execution hint inside the shell.
*/
func RouteHintForSymbol(symbol byte) uint8 {
	return byteRouteHints[int(symbol)]
}

/*
NeutralValue allocates a lexical-free native value. The value starts with an
identity affine operator so it behaves like a tiny local program even before
any lexical observable is projected onto it.
*/
func NeutralValue() Value {
	value := MustNewValue()
	value.SetAffine(1, 0)
	return value
}

/*
SeedObservable projects a lexical seed onto an otherwise native value so
prompts and human-facing outputs remain decodable without forcing storage to
carry the byte twice.
*/
func SeedObservable(symbol byte, value Value) Value {
	return ObservableValue(symbol, value)
}

/*
SetStatePhase records the logical GF(257) state in both the core bit-field and
in ResidualCarry. Fresh native values should use this helper instead of setting
raw bits directly so the state/control split stays explicit.
*/
func (value *Value) SetStatePhase(phase numeric.Phase) {
	if phase == 0 {
		value.SetResidualCarry(0)
		return
	}

	value.Set(int(phase))
	value.SetResidualCarry(uint64(phase))
}

/*
SetAffine stores a tiny affine operator f(x) = ax + b (mod 257) in the shell.
This lets each value behave like a local transition rule rather than a passive
payload. Scale zero is normalized to the identity because traversal wants an
invertible default, not a black hole.
*/
func (value *Value) SetAffine(scale, translate numeric.Phase) {
	s := normalizePhaseWord(scale)
	if s == 0 {
		s = 1
	}
	t := normalizePhaseWord(translate)

	word := value.C7()
	word &^= affineWordMaskScale | affineWordMaskTranslate
	word |= s << affineWordShiftScale
	word |= t << affineWordShiftTranslate
	value.SetC7(word)
}

/*
Affine retrieves the affine operator stored in the shell. Missing scale data is
interpreted as identity so older values without shell operators remain valid.
*/
func (value *Value) Affine() (numeric.Phase, numeric.Phase) {
	scale := numeric.Phase((value.C7() & affineWordMaskScale) >> affineWordShiftScale)
	if scale == 0 {
		scale = 1
	}
	translate := numeric.Phase((value.C7() & affineWordMaskTranslate) >> affineWordShiftTranslate)
	return scale, translate
}

/*
HasAffine reports whether the value explicitly carries an affine operator.
Legacy values without a stored scale/translate return false.
*/
func (value *Value) HasAffine() bool {
	return value.C7()&(affineWordMaskScale|affineWordMaskTranslate) != 0
}

/*
ApplyAffinePhase advances a phase through the value's local affine operator.
*/
func (value *Value) ApplyAffinePhase(phase numeric.Phase) numeric.Phase {
	scale, translate := value.Affine()
	return numeric.Phase((uint32(scale)*uint32(phase) + uint32(translate)) % numeric.FermatPrime)
}

/*
OperatorFlags exposes the shell-level execution flags packed into the upper 12
bits of the affine word.
*/
func (value *Value) OperatorFlags() uint16 {
	return uint16((value.C7() & shellWordMaskFlags) >> shellWordShiftFlags)
}

func (value *Value) setOperatorFlags(flags uint16) {
	word := value.C7()
	word &^= shellWordMaskFlags
	word |= uint64(flags&0x0FFF) << shellWordShiftFlags
	value.SetC7(word)
}

func (value *Value) setOperatorFlag(flag uint16, enabled bool) {
	flags := value.OperatorFlags()
	if enabled {
		flags |= flag
	} else {
		flags &^= flag
	}
	value.setOperatorFlags(flags)
}

/*
HasOperatorFlag reports whether the requested shell-level execution flag is set.
*/
func (value *Value) HasOperatorFlag(flag uint16) bool {
	return value.OperatorFlags()&flag != 0
}

/*
SetMutable marks the value as logically mutable. This does not mutate storage in
place; it merely records that the operator may be versioned append-only in the
LSM when updated.
*/
func (value *Value) SetMutable(mutable bool) {
	value.setOperatorFlag(ValueFlagMutable, mutable)
}

/*
Mutable reports whether the value has been marked as logically mutable.
*/
func (value *Value) Mutable() bool {
	return value.HasOperatorFlag(ValueFlagMutable)
}

/*
SetTrajectory stores a phase-to-phase snapshot of the operator's intended
continuation. This lets traversal prefer an observed local orbit over a generic
affine extrapolation when the current state still matches the stored source.
*/
func (value *Value) SetTrajectory(from, to numeric.Phase) {
	word := value.C7()
	word &^= shellWordMaskTrajectoryFrom | shellWordMaskTrajectoryTo
	word |= normalizePhaseWord(from) << shellWordShiftTrajectoryFrom
	word |= normalizePhaseWord(to) << shellWordShiftTrajectoryTo
	value.SetC7(word)
	value.setOperatorFlag(ValueFlagTrajectory, true)
}

/*
Trajectory retrieves the stored phase snapshot. It returns ok=false when the
value does not explicitly carry a trajectory snapshot in its shell.
*/
func (value *Value) Trajectory() (numeric.Phase, numeric.Phase, bool) {
	if !value.HasTrajectory() {
		return 0, 0, false
	}

	from := numeric.Phase((value.C7() & shellWordMaskTrajectoryFrom) >> shellWordShiftTrajectoryFrom)
	to := numeric.Phase((value.C7() & shellWordMaskTrajectoryTo) >> shellWordShiftTrajectoryTo)
	return from, to, true
}

/*
HasTrajectory reports whether the value carries an explicit trajectory snapshot.
*/
func (value *Value) HasTrajectory() bool {
	return value.HasOperatorFlag(ValueFlagTrajectory)
}

/*
SetGuardRadius stores a tolerated modular phase drift for the next hop. A guard
radius of zero means the operator expects exact continuation.
*/
func (value *Value) SetGuardRadius(radius uint8) {
	word := value.C7()
	word &^= shellWordMaskGuardRadius
	word |= uint64(radius) << shellWordShiftGuardRadius
	value.SetC7(word)
	value.setOperatorFlag(ValueFlagGuard, true)
}

/*
GuardRadius retrieves the stored modular drift budget for the next hop.
*/
func (value *Value) GuardRadius() uint8 {
	return uint8((value.C7() & shellWordMaskGuardRadius) >> shellWordShiftGuardRadius)
}

/*
HasGuard reports whether the value explicitly carries a transition guard.
*/
func (value *Value) HasGuard() bool {
	return value.HasOperatorFlag(ValueFlagGuard)
}

/*
SetRouteHint stores a compact route class that biases the next hop toward
compatible continuation cells without redundantly storing a lexical byte.
*/
func (value *Value) SetRouteHint(route uint8) {
	word := value.C7()
	word &^= shellWordMaskRouteHint
	word |= uint64(route) << shellWordShiftRouteHint
	value.SetC7(word)
	value.setOperatorFlag(ValueFlagRouteHint, true)
}

/*
RouteHint retrieves the shell-level continuation class carried by the value.
*/
func (value *Value) RouteHint() uint8 {
	return uint8((value.C7() & shellWordMaskRouteHint) >> shellWordShiftRouteHint)
}

/*
HasRouteHint reports whether the value carries a route hint.
*/
func (value *Value) HasRouteHint() bool {
	return value.HasOperatorFlag(ValueFlagRouteHint)
}

/*
SetLexicalTransition installs the native operator that would be induced by the
next byte under the original lexical phase rule. This preserves compatibility
with existing prompt/state logic while keeping the stored value lexical-free.

When the current state snapshot is already available in ResidualCarry, the value
also records a trajectory snapshot and a compact route hint in the shell so the
operator can steer traversal without replaying lexical identity from storage.
*/
func (value *Value) SetLexicalTransition(next byte) {
	value.SetAffine(PhaseScaleForByte(next), 0)
	value.SetRouteHint(RouteHintForSymbol(next))

	if carry := value.ResidualCarry(); carry > 0 {
		current := numeric.Phase(carry % uint64(numeric.FermatPrime))
		if current > 0 {
			value.SetTrajectory(current, value.ApplyAffinePhase(current))
		}
	}
}

func initByteValueMultipliers() [256]int {
	var multipliers [256]int
	for b := range len(multipliers) {
		multipliers[b] = modPow257(3, b+1)
	}
	return multipliers
}

func modPow257(base, exp int) int {
	result := 1
	base %= 257
	for exp > 0 {
		if exp&1 == 1 {
			result = (result * base) % 257
		}
		base = (base * base) % 257
		exp >>= 1
	}
	return result
}

func baseValueOffsets(b byte) [5]int {
	mul := byteValueMultipliers[int(b)]
	add := (int(b)*17 + 1) % 257

	var offsets [5]int
	for i, base := range canonicalValueBasis {
		offsets[i] = (base*mul + add) % 257
	}
	return offsets
}

/*
HasLexicalSeed reports whether the value visibly contains the five-bit
lexical seed for the supplied byte. Query observables should usually return
true; canonical stored values should usually return false.
*/
func HasLexicalSeed(value Value, b byte) bool {
	base := BaseValue(b)
	return ValueSimilarity(&base, &value) == base.ActiveCount()
}

/*
ObservableValue projects a stored native value back into the lexical plane for
measurement and human-facing decode. The key already names the byte; this helper
simply re-injects the byte's transient five-bit seed without disturbing the
stored phase or control shell.
*/
func ObservableValue(symbol byte, value Value) Value {
	out := MustNewValue()
	out.CopyFrom(value)

	for _, off := range baseValueOffsets(symbol) {
		out.Set(off)
	}

	return out
}

/*
StorageValue removes the transient lexical seed from an observable value so the
persistent LSM value can stay native. Byte identity lives in the Morton key;
the stored value keeps phase, control metadata, and any higher-dimensional shell.
*/
func StorageValue(symbol byte, observable Value) Value {
	out := MustNewValue()
	out.CopyFrom(observable)

	if HasLexicalSeed(observable, symbol) {
		for _, off := range baseValueOffsets(symbol) {
			out.Clear(off)
		}
	}

	if carry := observable.ResidualCarry(); carry > 0 {
		phase := numeric.Phase(carry % uint64(numeric.FermatPrime))
		if phase > 0 {
			out.Set(int(phase))
		}
	}

	return out
}

func BuildValue(payload []byte) (Value, error) {
	console.Trace("Building value", "payload", string(payload))

	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		return Value{}, err
	}

	value, err := NewRootValue(seg)
	if err != nil {
		return Value{}, err
	}

	for pos, b := range payload {
		positioned := BaseValue(b)
		positioned = positioned.RollLeft(pos)
		value = value.OR(positioned)
	}

	return value, nil
}

/*
CopyFrom copies all 8 words from src into the receiver.
Replaces the repeated SetC0..SetC7 call pattern at every call site.
*/
func (value *Value) CopyFrom(src Value) {
	for i := range 8 {
		value.setBlock(i, src.block(i))
	}
}

/*
ValueSliceToList packs a Go slice of Values into a Cap'n Proto Value_List.
*/
func ValueSliceToList(values []Value) (Value_List, error) {
	_, seg, err := capnp.NewMessage(capnp.MultiSegment(nil))

	if err != nil {
		return Value_List{}, err
	}

	list, err := NewValue_List(seg, int32(len(values)))

	if err != nil {
		return Value_List{}, err
	}

	for i, cc := range values {
		dst := list.At(i)
		dst.CopyFrom(cc)
	}

	return list, nil
}

/*
ValueListToSlice copies each entry into a freshly allocated Value and returns the slice.
*/
func ValueListToSlice(list Value_List) ([]Value, error) {
	out := make([]Value, list.Len())

	for i := 0; i < list.Len(); i++ {
		src := list.At(i)

		_, seg, err := capnp.NewMessage(capnp.MultiSegment(nil))

		if err != nil {
			return nil, err
		}

		value, err := NewValue(seg)

		if err != nil {
			return nil, err
		}

		value.CopyFrom(src)
		out[i] = value
	}

	return out, nil
}

/*
Sanitize enforces the lower 257-bit field width for fundamental field comparisons,
but preserves the upper 255 bits (Guard Band) which is now utilized for
Cross-Modal Alignment, Rotational Opcodes, and Residual Phase Carry.
*/
func (value *Value) Sanitize() {
	value.SetC4(value.C4() & 1) // Bit 256 is the delimiter
	// Words 5, 6, and 7 are deliberately kept alive as the Guard Band for Opcodes
	// See SetOpcode and SetResidualCarry.
}

/*
SetOpcode stores the low 8-bit program opcode in the Guard Band while preserving
all other control-plane fields packed into word 5.
*/
func (value *Value) SetOpcode(opcode uint64) {
	word := value.C5()
	word &^= 0xFF
	word |= opcode & 0xFF
	value.SetC5(word)
}

/*
Opcode retrieves the low 8-bit program opcode embedded in the Guard Band.
*/
func (value *Value) Opcode() uint64 {
	return value.C5() & 0xFF
}

/*
SetResidualCarry stores fractional phase state across distributed wavefront computations (Word 6).
*/
func (value *Value) SetResidualCarry(carry uint64) {
	value.SetC6(carry)
}

/*
ResidualCarry retrieves fractional phase context stored in the Guard Band.
*/
func (value *Value) ResidualCarry() uint64 {
	return value.C6()
}

func (value *Value) Block(i int) uint64 {
	return value.block(i)
}

func (value *Value) block(i int) uint64 {
	switch i {
	case 0:
		return value.C0()
	case 1:
		return value.C1()
	case 2:
		return value.C2()
	case 3:
		return value.C3()
	case 4:
		return value.C4()
	case 5:
		return value.C5()
	case 6:
		return value.C6()
	case 7:
		return value.C7()
	default:
		return 0
	}
}

func (value *Value) setBlock(i int, v uint64) {
	switch i {
	case 0:
		value.SetC0(v)
	case 1:
		value.SetC1(v)
	case 2:
		value.SetC2(v)
	case 3:
		value.SetC3(v)
	case 4:
		value.SetC4(v)
	case 5:
		value.SetC5(v)
	case 6:
		value.SetC6(v)
	case 7:
		value.SetC7(v)
	}
}

/*
Has checks if the prime at index p is active in the value.
*/
func (value *Value) Has(p int) bool {
	return (value.block(p/64) & (1 << (p % 64))) != 0
}

/*
Set activates the prime at index p.
*/
func (value *Value) Set(p int) {
	value.setBlock(p/64, value.block(p/64)|(1<<(p%64)))
}

/*
Clear deactivates the prime at index p.
*/
func (value *Value) Clear(p int) {
	value.setBlock(p/64, value.block(p/64)&^(1<<(p%64)))
}

/*
RotationSeed derives a structural affine seed from the value itself.
Unlike a popcount-only mapping, this uses the actual active prime layout so
distinct values with identical density can still drive different rotations.
*/
func (value *Value) RotationSeed() (uint16, uint16) {
	if value.ActiveCount() == 0 {
		return 1, 0
	}

	var accA uint32 = 1
	var accB uint32

	for blockIdx := range config.ValueBlocks {
		block := value.block(blockIdx)
		if block == 0 {
			continue
		}

		mix := uint32(block^(block>>29)^(block>>43)) & 0x1FFFF
		accA = (accA*131 + mix + uint32(blockIdx+1)*17) % 257
		accB = (accB*137 + mix + uint32(Popcount(block))*29 + uint32(blockIdx+1)*31) % 257

		for block != 0 {
			bitIdx := bits.TrailingZeros64(block)
			primeIdx := blockIdx*64 + bitIdx

			if primeIdx >= 257 {
				block &= block - 1
				continue
			}

			prime := uint32(primeIdx + 1)
			accA = (accA + prime*prime + prime*23 + uint32(bitIdx+1)*7) % 257
			accB = (accB + prime*67 + uint32(bitIdx+1)*13) % 257

			block &= block - 1
		}
	}

	if accA == 0 {
		accA = 1
	}

	return uint16(accA), uint16(accB % 257)
}

/*
BaseValue returns a deterministic 5-bit value for a byte value.
The value is generated by an affine transform of a sparse 5-mark basis
inside GF(257), so collisions remain structured and rotation-friendly
instead of behaving like arbitrary coprime scatter.
*/
func BaseValue(b byte) Value {
	value := MustNewValue()

	for _, off := range baseValueOffsets(b) {
		value.Set(off)
	}

	return value
}

/*
ShannonDensity returns the fraction of the 257 logical core bits that are active.
The Sequencer uses this to force a boundary before the value saturates.
Above ~0.40 (103 bits) the core field loses discriminative power. Shell bits do
not count toward this threshold because they live in the hardware jacket, not in
the Fermat execution field itself.
*/
func (value Value) ShannonDensity() float64 {
	return float64(value.CoreActiveCount()) / 257.0
}

/*
MaskValue returns a control-plane marker used to denote an unresolved gap or
masked region in a sequence without colliding with any lexical BaseValue.
*/
func MaskValue() Value {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		panic(fmt.Errorf("MaskValue allocation failed: %w", err))
	}
	value, err := NewValue(seg)
	if err != nil {
		panic(fmt.Errorf("MaskValue allocation failed: %w", err))
	}
	value.Set(config.Numeric.VocabSize)

	return value
}

/*
ValueLCM returns the element-wise OR of values — the LCM in prime exponent space.
Used for aggregating span values (words, sentences, n-grams).
*/
func ValueLCM(values []Value) (lcm Value) {
	lcm = MustNewValue()

	var c0, c1, c2, c3, c4 uint64

	for _, ch := range values {
		c0 |= ch.C0()
		c1 |= ch.C1()
		c2 |= ch.C2()
		c3 |= ch.C3()
		c4 |= ch.C4()
	}

	lcm.SetC0(c0)
	lcm.SetC1(c1)
	lcm.SetC2(c2)
	lcm.SetC3(c3)
	lcm.SetC4(c4 & 1)
	lcm.SetC5(0)
	lcm.SetC6(0)
	lcm.SetC7(0)

	return lcm
}

/*
CoreActiveCount returns the number of active bits in the lower 257-bit Fermat
core only. This deliberately ignores the shell/jacket bits so density and core
energy calculations are not distorted by operator metadata.
*/
func (value Value) CoreActiveCount() (n int) {
	return Popcount(value.C0()) + Popcount(value.C1()) + Popcount(value.C2()) + Popcount(value.C3()) +
		Popcount(value.C4()&1)
}

/*
ShellActiveCount returns the number of active bits in the 255-bit hardware
jacket used for control metadata, carries, and higher-dimensional operators.
*/
func (value Value) ShellActiveCount() int {
	return Popcount(value.C4()&^uint64(1)) + Popcount(value.C5()) + Popcount(value.C6()) + Popcount(value.C7())
}

/*
ActiveCount returns the number of active bits across the full 512-bit value.
This is useful for total energy/accounting, while CoreActiveCount should be
used whenever the caller explicitly means the GF(257) execution field.
*/
func (value Value) ActiveCount() (n int) {
	return value.CoreActiveCount() + value.ShellActiveCount()
}

/*
popcount counts the number of 1-bits in a uint64
*/
func Popcount(x uint64) (count int) {
	return bits.OnesCount64(x)
}

/*
ValuePrimeIndices returns the prime indices (0..NBasis-1) that are set in the value.
Used for debug output (which primes were assigned).
*/
func ValuePrimeIndices(value *Value) []int {
	var out []int

	for i := range config.ValueBlocks {
		block := value.block(i)

		for block != 0 {
			bitIdx := bits.TrailingZeros64(block)
			primeIdx := i*64 + bitIdx

			if primeIdx < config.NBasis {
				out = append(out, primeIdx)
			}

			block &= block - 1
		}
	}

	return out
}

/*
ANDErr returns the element-wise AND of two values (their GCD in
prime exponent space), checking allocation errors. Shared factors.
*/
func (value *Value) ANDErr(other Value) (Value, error) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		return Value{}, err
	}
	gcd, err := NewValue(seg)
	if err != nil {
		return Value{}, err
	}
	gcd.setBlock(0, value.block(0)&other.block(0))
	gcd.setBlock(1, value.block(1)&other.block(1))
	gcd.setBlock(2, value.block(2)&other.block(2))
	gcd.setBlock(3, value.block(3)&other.block(3))
	gcd.setBlock(4, value.block(4)&other.block(4))
	gcd.setBlock(5, value.block(5)&other.block(5))
	gcd.setBlock(6, value.block(6)&other.block(6))
	gcd.setBlock(7, value.block(7)&other.block(7))
	return gcd, nil
}

/*
AND returns the element-wise AND of two values (their GCD in
prime exponent space). Shared factors.
*/
func (value *Value) AND(other Value) Value {
	gcd, err := value.ANDErr(other)
	if err != nil {
		panic(err)
	}
	return gcd
}

/*
ValueBin maps a value to a structural bin 0..255 for indexing phase tables.
Deterministic XOR-fold of the value bits ensures similar values map to nearby bins.
Enables value-native co-occurrence and phase lookup without byte symbols.
*/
func ValueBin(c *Value) int {
	seeds := [8]uint64{
		0x9e3779b185ebca87,
		0xc2b2ae3d27d4eb4f,
		0x165667b19e3779f9,
		0x85ebca77c2b2ae63,
		0x27d4eb2f165667c5,
		0x94d049bb133111eb,
		0xd6e8feb86659fd93,
		0xa4093822299f31d1,
	}

	var acc [8]int

	for i := range config.ValueBlocks {
		block := c.block(i)
		for block != 0 {
			bit := bits.TrailingZeros64(block)
			idx := uint64(i*64 + bit + 1)

			for j := range seeds {
				h := idx*seeds[j] + (idx << uint(j+1))
				if h>>63 == 1 {
					acc[j]++
				} else {
					acc[j]--
				}
			}

			block &= block - 1
		}
	}

	var bin int
	for j := range acc {
		if acc[j] >= 0 {
			bin |= 1 << j
		}
	}

	return bin
}

/*
ValueSimilarity returns the number of shared prime exponents (popcount of AND).
*/
func ValueSimilarity(a, b *Value) (sim int) {
	return Popcount(a.C0()&b.C0()) + Popcount(a.C1()&b.C1()) + Popcount(a.C2()&b.C2()) + Popcount(a.C3()&b.C3()) +
		Popcount(a.C4()&b.C4()) + Popcount(a.C5()&b.C5()) + Popcount(a.C6()&b.C6()) + Popcount(a.C7()&b.C7())
}

/*
ValueHole returns target AND NOT existing — bits set in target but not in existing.
*/
func ValueHole(target, existing *Value) Value {
	hole := MustNewValue()
	hole.setBlock(0, target.block(0)&^existing.block(0))
	hole.setBlock(1, target.block(1)&^existing.block(1))
	hole.setBlock(2, target.block(2)&^existing.block(2))
	hole.setBlock(3, target.block(3)&^existing.block(3))
	hole.setBlock(4, target.block(4)&^existing.block(4))
	hole.setBlock(5, target.block(5)&^existing.block(5))
	hole.setBlock(6, target.block(6)&^existing.block(6))
	hole.setBlock(7, target.block(7)&^existing.block(7))
	return hole
}

/*
OR returns the element-wise OR of two values (their LCM in prime exponent space).
*/
func (value *Value) OR(other Value) Value {
	lcm := MustNewValue()

	lcm.SetC0(value.C0() | other.C0())
	lcm.SetC1(value.C1() | other.C1())
	lcm.SetC2(value.C2() | other.C2())
	lcm.SetC3(value.C3() | other.C3())
	lcm.SetC4((value.C4() | other.C4()) & 1)

	return lcm
}

/*
MustNewValue allocates a fresh zero-valued Value, panicking on allocation failure.
*/
func MustNewValue() Value {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		panic(fmt.Errorf("allocation failed: %w", err))
	}
	value, err := NewValue(seg)
	if err != nil {
		panic(fmt.Errorf("allocation failed: %w", err))
	}
	return value
}

/*
XOR returns the element-wise XOR of two values (for cancellative superposition).
*/
func (value Value) XOR(other Value) Value {
	xor := MustNewValue()

	xor.SetC0(value.C0() ^ other.C0())
	xor.SetC1(value.C1() ^ other.C1())
	xor.SetC2(value.C2() ^ other.C2())
	xor.SetC3(value.C3() ^ other.C3())
	xor.SetC4((value.C4() ^ other.C4()) & 1)

	return xor
}

/*
FlatValue is a dense array of active prime indices used for optimal GPU iteration.
It eliminates bit-twiddling thread divergence in SIMT architectures.
*/
type FlatValue struct {
	ActivePrimes [config.NBasis]uint16
	Count        uint16
	_            uint16 // Padding
}

/*
Flatten converts the sparse bitset into a densely packed array of active prime indices.
*/
func (value *Value) Flatten() FlatValue {
	var flat FlatValue

	for i := range config.ValueBlocks {
		block := value.block(i)

		for block != 0 {
			bitIdx := uint16(bits.TrailingZeros64(block))
			flat.ActivePrimes[flat.Count] = uint16(i*64) + bitIdx
			flat.Count++
			block &= block - 1
		}
	}

	return flat
}

/*
FlattenBatched converts a slice of sparse Values into a slice of FlatValues.
If a pool is provided, each value is scheduled as an independent task and the
pool's built-in scaler handles concurrency — no manual worker-count tuning.
Falls back to synchronous execution when no pool is available.
*/
func FlattenBatched(values []Value, p *pool.Pool) []FlatValue {
	n := len(values)
	out := make([]FlatValue, n)

	if n == 0 {
		return out
	}

	if p == nil {
		for i := range values {
			out[i] = values[i].Flatten()
		}
		return out
	}

	wg := sync.WaitGroup{}

	for i := range values {
		idx := i
		resCh := p.Schedule(fmt.Sprintf("flatten-%d", idx), func(ctx context.Context) (any, error) {
			out[idx] = values[idx].Flatten()
			return nil, nil
		})
		if resCh == nil {
			out[idx] = values[idx].Flatten()
			continue
		}
		wg.Add(1)
		go func(ch chan *pool.Result) {
			defer wg.Done()
			<-ch
		}(resCh)
	}

	wg.Wait()
	return out
}

/*
RollLeft circular-shifts the value within the 257-bit logical width.
Binds sequential position to geometry before superposition.
*/
func (value *Value) RollLeft(shift int) Value {
	if shift == 0 {
		return *value
	}

	const logicalBits = 257 // CubeFaces
	out := MustNewValue()
	shift = shift % logicalBits

	// Fast sparse-array permutation within the 257-bit logical width
	for i := range config.ValueBlocks {
		block := value.block(i)
		for block != 0 {
			bitIdx := uint16(bits.TrailingZeros64(block))
			primeIdx := i*64 + int(bitIdx)

			if primeIdx < logicalBits {
				newPrimeIdx := (primeIdx + shift) % logicalBits
				out.Set(newPrimeIdx)
			}

			block &= block - 1
		}
	}

	return out
}

/*
Rotate3D applies all three GF(257) axes in sequence.
X (Translation): p → (p + 1) mod 257
Y (Dilation):    p → (3·p) mod 257
Z (Affine):      p → (3·p + 1) mod 257
Combined orbit ~17M states. 3 is a primitive root of 257.
*/
func (value *Value) Rotate3D() Value {
	const logicalBits = 257

	out := MustNewValue()

	for i := range config.ValueBlocks {
		block := value.block(i)

		for block != 0 {
			bitIdx := uint16(bits.TrailingZeros64(block))
			primeIdx := i*64 + int(bitIdx)

			if primeIdx < logicalBits {
				p := (primeIdx + 1) % logicalBits
				p = (3 * p) % logicalBits
				p = (3*p + 1) % logicalBits

				out.Set(p)
			}

			block &= block - 1
		}
	}

	return out
}
