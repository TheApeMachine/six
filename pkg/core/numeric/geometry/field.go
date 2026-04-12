package geometry

import (
	"math"
	"math/bits"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
Field is one phase vector over GF(p) for a fixed modulus p. The vector has
exactly p lanes — one per residue 0..p−1 on the same ring as the affine maps.
There is a single implementation: the three layers use fixed primes p —
GF(257) and GF(65537) (Fermat primes 2^8+1 and 2^16+1) and GF(8191) (Mersenne
prime 2^13−1). Observation, rotation, projection from a child field, dot
interference, and dominant extraction are identical operations at every layer
— nothing here is trie-only or routing-only.
*/
type Field struct {
	modulus      uint32
	Fields       []*Field
	Values       []*primitive.Value
	Affinity     []uint64
	amplitude    uint32
	Cooccurrence map[int]map[int]uint32
	lastMode     int
}

/*
Standard prime moduli for stacked GF(p) layers: 257 and 65537 are Fermat
primes; 8191 is the Mersenne prime 2^13−1. Same operations at each layer.
*/
const (
	Mod257   uint32 = 257
	Mod8191  uint32 = 8191
	Mod65537 uint32 = 65537
)

/*
LaneCountForModulus returns the phase-vector width for GF(p): p lanes, indexed
by residues 0..p−1. This matches the affine slot geometry at each tier.
*/
func LaneCountForModulus(modulus uint32) int {
	if modulus == 0 {
		return 0
	}

	return int(modulus)
}

/*
NewField allocates a zeroed phase vector with one lane per GF(p) residue;
modulus selects p and the reduction path (257, 8191, or 65537).
*/
func NewField(modulus uint32) *Field {
	return newFieldLanes(LaneCountForModulus(modulus), modulus)
}

func newFieldLanes(laneCount int, modulus uint32) *Field {
	if laneCount < 1 {
		laneCount = LaneCountForModulus(modulus)
	}

	if laneCount < 1 {
		return &Field{
			modulus:      modulus,
			Fields:       nil,
			Cooccurrence: make(map[int]map[int]uint32),
			lastMode:     -1,
		}
	}

	return &Field{
		modulus:      modulus,
		Fields:       make([]*Field, laneCount),
		Cooccurrence: make(map[int]map[int]uint32),
		lastMode:     -1,
	}
}

func positiveMod(value int, modulus int) int {
	if modulus < 1 {
		return 0
	}

	remainder := value % modulus

	if remainder < 0 {
		remainder += modulus
	}

	return remainder
}

/*
Modulus returns p for GF(p).
*/
func (field *Field) Modulus() uint32 {
	if field == nil {
		return 0
	}

	return field.modulus
}

/*
MergeAffinity XORs the incoming affinity into the field's Affinity slice.
If the field's Affinity is empty, it initializes it.
*/
func (field *Field) MergeAffinity(valueAffinity []uint64) {
	if field == nil || len(valueAffinity) == 0 {
		return
	}

	if len(field.Affinity) == 0 {
		field.Affinity = make([]uint64, len(valueAffinity))
		copy(field.Affinity, valueAffinity)
		return
	}

	minLen := len(field.Affinity)
	if len(valueAffinity) < minLen {
		minLen = len(valueAffinity)
	}

	for i := 0; i < minLen; i++ {
		field.Affinity[i] ^= valueAffinity[i]
	}
}

/*
AffinitySaturation calculates the popcount of the Affinity slice divided by 257.
*/
func (field *Field) AffinitySaturation() float64 {
	if field == nil || len(field.Affinity) == 0 {
		return 0
	}

	var popcount int
	for _, word := range field.Affinity {
		popcount += bits.OnesCount64(word)
	}

	return float64(popcount) / 257.0
}

/*
Rotate applies a whole-vector affine phase rotation: each lane i maps
v ↦ a·v + (b + i + 1) in GF(modulus), matching the prior per-tier behavior.
*/
func (field *Field) Rotate(multiplier uint32, bias uint32) {
	if field == nil {
		return
	}

	if len(field.Fields) == 0 {
		field.amplitude = field.affineMod(field.amplitude, multiplier, bias)
		return
	}

	for laneIndex := range field.Fields {
		laneBias := field.addMod(bias, uint32(laneIndex+1))
		field.Fields[laneIndex].Rotate(multiplier, laneBias)
	}
}

/*
Rewind reverses an affine phase rotation. It is the exact inverse of Rotate,
allowing the field to backtrack and explore alternative trajectories.
*/
func (field *Field) Rewind(multiplier uint32, bias uint32) {
	if field == nil {
		return
	}

	if len(field.Fields) == 0 {
		inv := field.inverseMod(multiplier)
		val := field.subMod(field.amplitude, bias)
		field.amplitude = field.mulMod(val, inv)
		return
	}

	for laneIndex := range field.Fields {
		laneBias := field.addMod(bias, uint32(laneIndex+1))
		field.Fields[laneIndex].Rewind(multiplier, laneBias)
	}
}

/*
Dominant returns the strongest lane and mass-normalized concentration.
*/
func (field *Field) Dominant() PhaseMode {
	if field == nil {
		return PhaseMode{Index: -1}
	}

	if len(field.Fields) == 0 {
		return PhaseMode{
			Index:         -1,
			Amplitude:     field.amplitude,
			Concentration: 1.0,
		}
	}

	var totalMass uint64

	bestIndex := -1

	var bestSignal uint32

	for laneIndex, laneValue := range field.Fields {
		if laneValue == nil {
			continue
		}

		laneDominant := laneValue.Dominant()
		totalMass += uint64(laneDominant.Amplitude)

		if laneDominant.Amplitude > bestSignal {
			bestIndex = laneIndex
			bestSignal = laneDominant.Amplitude
		}
	}

	if totalMass == 0 {
		return PhaseMode{Index: -1}
	}

	if field.lastMode != -1 && bestIndex != -1 && field.lastMode != bestIndex {
		if field.Cooccurrence[field.lastMode] == nil {
			field.Cooccurrence[field.lastMode] = make(map[int]uint32)
		}
		field.Cooccurrence[field.lastMode][bestIndex]++
	}

	if bestIndex != -1 {
		field.lastMode = bestIndex
	}

	return PhaseMode{
		Index:         bestIndex,
		Amplitude:     bestSignal,
		Concentration: float64(bestSignal) / float64(totalMass),
	}
}

/*
Alignment scores circular agreement between two lane indices; same ring math
as LaneRingAlignment for this field’s lane count.
*/
func (field *Field) Alignment(leftIndex int, rightIndex int) float64 {
	if field == nil {
		return 0
	}

	return LaneRingAlignment(leftIndex, rightIndex, len(field.Fields))
}

func (field *Field) addMod(left uint32, right uint32) uint32 {
	sum := uint64(left) + uint64(right)

	return field.reduceU64(sum)
}

func (field *Field) subMod(left uint32, right uint32) uint32 {
	if left >= right {
		return left - right
	}
	return field.modulus - ((right - left) % field.modulus)
}

func (field *Field) mulMod(left uint32, right uint32) uint32 {
	prod := uint64(left) * uint64(right)

	return field.reduceU64(prod)
}

func (field *Field) inverseMod(a uint32) uint32 {
	if a == 0 {
		return 0
	}

	res := uint64(1)
	base := uint64(a)
	exp := uint64(field.modulus - 2)

	for exp > 0 {
		if exp%2 == 1 {
			res = (res * base) % uint64(field.modulus)
		}
		base = (base * base) % uint64(field.modulus)
		exp /= 2
	}

	return uint32(res)
}

func (field *Field) affineMod(value uint32, multiplier uint32, bias uint32) uint32 {
	return field.addMod(field.mulMod(value, multiplier), bias)
}

func (field *Field) reduceU32(value uint32) uint32 {
	return field.reduceU64(uint64(value))
}

func (field *Field) reduceU64(value uint64) uint32 {
	switch field.modulus {
	case Mod257:
		return reduce257Uint64(value)
	case Mod8191:
		return reduce8191Uint64(value)
	case Mod65537:
		return reduce65537Uint64(value)
	default:
		return uint32(value % uint64(field.modulus))
	}
}

/*
reduce257Uint64 exploits 256 ≡ −1 (mod 257): fold all eight bytes with
alternating sign so bits 0–63 participate in the residue.
*/
func reduce257Uint64(value uint64) uint32 {
	var acc int64

	for byteIdx := range 8 {
		b := int64((value >> (8 * byteIdx)) & 0xff)

		if byteIdx%2 == 0 {
			acc += b
		} else {
			acc -= b
		}
	}

	for acc < 0 {
		acc += int64(Mod257)
	}

	for acc >= int64(Mod257) {
		acc -= int64(Mod257)
	}

	return uint32(acc)
}

/*
reduce8191Uint64 exploits 2^13 ≡ 1 (mod 8191): fold the full uint64 with
the same low/high 13-bit split until the residue fits.
*/
func reduce8191Uint64(value uint64) uint32 {
	const mask13 = uint64(0x1FFF)

	reduced := value

	for reduced > uint64(Mod8191) {
		reduced = (reduced & mask13) + (reduced >> 13)
	}

	if reduced == uint64(Mod8191) {
		return 0
	}

	return uint32(reduced)
}

/*
reduce65537Uint64 exploits 2^16 ≡ −1 (mod 65537): combine four 16-bit limbs
so bits 0–63 all contribute (x0 − x1 + x2 − x3).
*/
func reduce65537Uint64(value uint64) uint32 {
	x0 := int64(value & 0xffff)
	x1 := int64((value >> 16) & 0xffff)
	x2 := int64((value >> 32) & 0xffff)
	x3 := int64((value >> 48) & 0xffff)

	reduced := x0 - x1 + x2 - x3

	for reduced < 0 {
		reduced += int64(Mod65537)
	}

	for reduced >= int64(Mod65537) {
		reduced -= int64(Mod65537)
	}

	return uint32(reduced)
}

/*
AccumulateProjected adds the child field into the receiver, projecting each
lane through the same slot-dependent map used when folding a lower layer into
the next: Mod8191 ← Mod257, Mod65537 ← Mod8191. The pair (receiver modulus,
child modulus) must match one of those two steps; otherwise the call is a
no-op.
*/
func (field *Field) AccumulateProjected(child *Field, slot int) {
	if field == nil || child == nil {
		return
	}

	parentLanes := len(field.Fields)
	childLanes := len(child.Fields)

	if parentLanes == 0 || childLanes == 0 || childLanes > parentLanes {
		return
	}

	switch {
	case field.modulus == Mod8191 && child.modulus == Mod257:
		// Embed GF(257) along the residue prefix of GF(8191).
	case field.modulus == Mod65537 && child.modulus == Mod8191:
		// Embed GF(8191) along the residue prefix of GF(65537).
	default:
		return
	}

	width := parentLanes

	slotMultiplier := field.reduceU32(uint32((slot % width) + 1))

	if slotMultiplier == 0 {
		slotMultiplier = 1
	}

	for laneIndex := range childLanes {
		childValue := child.Fields[laneIndex].Dominant().Amplitude

		if childValue == 0 {
			continue
		}

		laneMultiplier := field.addMod(slotMultiplier, uint32(laneIndex+1))
		projected := field.mulMod(laneMultiplier, childValue)
		field.addMod(field.Fields[laneIndex].Dominant().Amplitude, projected)
	}
}

/*
Dot returns sum_i (receiver_i · other_i) in GF(modulus). Both fields must share
the same modulus and lane count.
*/
func (field *Field) Dot(other *Field) uint32 {
	if field == nil || other == nil {
		return 0
	}

	if field.modulus != other.modulus || len(field.Fields) != len(other.Fields) {
		return 0
	}

	var accumulator uint32

	for laneIndex := range field.Fields {
		product := field.mulMod(
			field.Fields[laneIndex].Dominant().Amplitude,
			other.Fields[laneIndex].Dominant().Amplitude,
		)
		
		accumulator = field.addMod(accumulator, product)
	}

	return accumulator
}

/*
Clone returns an independent copy of the field (same modulus and lanes).
*/
func (field *Field) Clone() *Field {
	if field == nil {
		return nil
	}

	out := newFieldLanes(len(field.Fields), field.modulus)
	copy(out.Fields, field.Fields)

	return out
}

/*
AggregateFromLowerFields folds each child into the receiver using
AccumulateProjected with slot = slotBase + index. Skips nil children.
Same operation whether aggregating GF(257)→GF(8191) or GF(8191)→GF(65537).
*/
func (field *Field) AggregateFromLowerFields(children []*Field, slotBase int) {
	if field == nil {
		return
	}

	for offset, child := range children {
		if child == nil {
			continue
		}

		field.AccumulateProjected(child, slotBase+offset)
	}
}

/*
ReduceScalar maps an integer into GF(modulus) using the same reductions as Field.
*/
func ReduceScalar(modulus uint32, value uint64) uint32 {
	switch modulus {
	case Mod257:
		return reduce257Uint64(value)
	case Mod8191:
		return reduce8191Uint64(value)
	case Mod65537:
		return reduce65537Uint64(value)
	default:
		if modulus == 0 {
			return 0
		}

		return uint32(value % uint64(modulus))
	}
}

/*
Gain maps a GF residue to [0,1] using (modulus−1) as the ceiling.
*/
func (field *Field) Gain(interference uint32) float64 {
	if field == nil || field.modulus < 2 {
		return 0
	}

	return float64(interference) / float64(field.modulus-1)
}

/*
InterferenceMultiplier turns phase alignment and signal strength into an
exponential beam/training bias (same at every layer).
*/
func InterferenceMultiplier(alignment float64, gain float64) float64 {
	if gain <= 0 {
		return 1
	}

	if alignment < 0 {
		alignment = 0
	}

	if alignment > 1 {
		alignment = 1
	}

	return math.Exp(((alignment * 2) - 1) * gain * 4)
}

/*
LaneRingAlignment is circular distance on a ring of laneCount lanes. It does
not depend on which tier produced the indices — only on ring geometry.
*/
func LaneRingAlignment(leftIndex int, rightIndex int, laneCount int) float64 {
	if laneCount < 1 {
		return 0
	}

	if leftIndex < 0 || rightIndex < 0 {
		return 0
	}

	distance := leftIndex - rightIndex

	if distance < 0 {
		distance = -distance
	}

	half := laneCount / 2

	if distance > half {
		distance = laneCount - distance
	}

	if half == 0 {
		return 1
	}

	return 1.0 - (float64(distance) / float64(half))
}

/*
Rotation represents a single affine rotation step.
*/
type Rotation struct {
	Multiplier uint32
	Bias       uint32
}

/*
Phasedial acts as a perspective alignment device. It maintains a rotation
state that can be dialed forward or backward to shift the field's perspective,
allowing for backtracking and exploring alternative trajectories.
*/
type Phasedial struct {
	field   *Field
	history []Rotation
}

/*
NewPhasedial initializes a new perspective alignment device for a field.
*/
func NewPhasedial(field *Field) *Phasedial {
	return &Phasedial{
		field:   field,
		history: make([]Rotation, 0),
	}
}

/*
Dial applies an affine rotation and records it in history.
*/
func (pd *Phasedial) Dial(multiplier uint32, bias uint32) {
	if pd == nil || pd.field == nil {
		return
	}

	pd.history = append(pd.history, Rotation{Multiplier: multiplier, Bias: bias})
	pd.field.Rotate(multiplier, bias)
}

/*
Rewind reverses the last affine rotation, popping it from history.
*/
func (pd *Phasedial) Rewind() {
	if pd == nil || pd.field == nil || len(pd.history) == 0 {
		return
	}

	last := pd.history[len(pd.history)-1]
	pd.history = pd.history[:len(pd.history)-1]

	pd.field.Rewind(last.Multiplier, last.Bias)
}
