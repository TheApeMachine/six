package geometry

import (
	"io"
	"math"
	"math/bits"
	"sync"

	"github.com/theapemachine/six/pkg/core/data"
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
	Children     []*Field
	Values       []*primitive.Value
	Affinity     []uint64
	amplitude    uint32
	Cooccurrence map[int]map[int]uint32
	lastMode     int

	intake *data.Ring
	output *data.Ring
	peers  []io.ReadWriteCloser
	initIO sync.Once
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

const (
	valueIDWord        = 122
	valueAffinityStart = 123
	valueAffinityWords = 5

	resonanceMinMembers    = 3
	resonanceConcentration = 0.6
)

/*
Cycle runs one physics step on the field and returns every Value the field
has processed. Leaf fields (communities with no lanes) run eigenmode detection
over their Value population and apply rotational pressure toward the dominant
cluster. Root fields cycle each community child, then aggregate upward and
apply top-down corrective pressure on misaligned communities.
*/
func (field *Field) Cycle() ([]*primitive.Value, error) {
	if field == nil {
		return nil, nil
	}

	if len(field.Children) > 0 {
		return field.cycleRoot()
	}

	if len(field.Values) > 0 || len(field.Fields) == 0 {
		return field.cycleLeaf()
	}

	return nil, nil
}

/*
cycleLeaf handles a community (leaf field with Values, no sub-lanes).
It detects eigenmodes from the population's affinity coupling, tracks the
dominant cluster's energy, and applies an affine rotation so the community's
phase state drifts toward its strongest internal agreement.
*/
func (field *Field) cycleLeaf() ([]*primitive.Value, error) {
	field.DrainIntake()

	if len(field.Values) == 0 {
		return nil, nil
	}

	field.ResetLanes()
	field.resetAffinity()

	affinityMap := make(map[uint64][]uint64, len(field.Values))
	participants := make([]ModeParticipant, 0, len(field.Values))

	for _, value := range field.Values {
		if value == nil {
			continue
		}

		id := (*value)[valueIDWord]
		aff := valueAffinity(value)
		affinityMap[id] = aff
		field.MergeAffinity(aff)
		field.observeAffinity(aff, 1)

		gap := field.BeliefGap(aff)
		energy := 1.0 - gap

		if energy < 0.01 {
			energy = 0.01
		}

		participants = append(participants, ModeParticipant{
			Origin: id,
			Energy: energy,
		})
	}

	if len(participants) == 0 {
		return field.Values, nil
	}

	modes, dominantIdx := DetectModes(
		participants,
		0.3,
		func(originA, originB uint64) float64 {
			affA := affinityMap[originA]
			affB := affinityMap[originB]

			if len(affA) == 0 || len(affB) == 0 {
				return 0
			}

			distance := AffinityHammingDistance(affA, affB)
			return 1.0 - float64(distance)/257.0
		},
	)

	if dominantIdx >= 0 && len(modes) > 0 {
		dominant := modes[dominantIdx]
		memberCount := uint32(len(dominant.Members()))

		multiplier := field.reduceU32(memberCount + 2)

		if multiplier == 0 {
			multiplier = 1
		}

		field.amplitude = field.affineMod(field.amplitude, multiplier, memberCount)
	}

	return field.Values, nil
}

/*
cycleRoot handles a root or intermediate field that owns community children.
Each community is cycled for its local physics, then the root aggregates:
Dominant() over all populated children yields the global mode. Communities
whose slot alignment with the global dominant falls below 0.5 receive a
corrective rotation — the top-down pressure that steers divergent pockets
back toward global coherence.
*/
func (field *Field) cycleRoot() ([]*primitive.Value, error) {
	var processed []*primitive.Value

	field.ResetLanes()
	field.resetAffinity()

	for _, community := range field.Children {
		if community == nil {
			continue
		}

		values, err := community.Cycle()
		if err != nil {
			return nil, err
		}

		processed = append(processed, values...)
		field.MergeAffinity(community.Affinity)
	}

	field.AggregateFromLowerFields(field.Children, 0)

	globalDominant := field.Dominant()

	if globalDominant.Index < 0 || globalDominant.Concentration <= 0.3 {
		return processed, nil
	}

	for slotIndex, community := range field.Children {
		if community == nil || len(community.Values) == 0 {
			continue
		}

		alignment := field.Alignment(slotIndex, globalDominant.Index)

		if alignment >= 0.5 {
			continue
		}

		multiplier := field.reduceU32(uint32(globalDominant.Index + 1))

		if multiplier == 0 {
			multiplier = 1
		}

		community.amplitude = community.affineMod(community.amplitude, multiplier, 1)
	}

	return processed, nil
}

/*
BeliefGap measures the normalised Hamming distance between the field's
current aggregate affinity and a target affinity vector. Returns [0, 1]
where 0 means the field's belief perfectly matches the target.
*/
func (field *Field) BeliefGap(targetAffinity []uint64) float64 {
	if field == nil {
		return 1.0
	}

	distance := AffinityHammingDistance(field.Affinity, targetAffinity)
	return float64(distance) / 257.0
}

/*
EmissionReady reports whether this community has accumulated enough
members with sufficient phase concentration to emit an action Value.
*/
func (field *Field) EmissionReady() bool {
	if field == nil || len(field.Values) < resonanceMinMembers {
		return false
	}

	dominant := field.Dominant()
	return dominant.Concentration >= resonanceConcentration
}

/*
MemberCount returns the number of Values currently held by this field.
*/
func (field *Field) MemberCount() int {
	if field == nil {
		return 0
	}

	return len(field.Values)
}

/*
valueAffinity extracts the 5-word affinity region directly from the wire frame.
Uses the fixed Value layout (words 123–127) so the geometry package stays
free of config imports.
*/
func valueAffinity(value *primitive.Value) []uint64 {
	out := make([]uint64, valueAffinityWords)

	for wordIndex := range out {
		out[wordIndex] = (*value)[valueAffinityStart+wordIndex]
	}

	return out
}

func (field *Field) resetAffinity() {
	if field == nil || len(field.Affinity) == 0 {
		return
	}

	clear(field.Affinity)
}

func (field *Field) observeAffinity(valueAffinity []uint64, weight uint32) {
	if field == nil || len(field.Fields) == 0 || weight == 0 || len(valueAffinity) == 0 {
		return
	}

	var folded uint64

	for wordIndex, word := range valueAffinity {
		shift := (wordIndex * 11) & 63
		folded ^= bits.RotateLeft64(word, shift)
		folded += uint64(wordIndex + 1)
	}

	field.Observe(int(ReduceScalar(field.modulus, folded)), weight)
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
		Children:     nil,
		Cooccurrence: make(map[int]map[int]uint32),
		lastMode:     -1,
	}
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
	if field == nil {
		return 0
	}

	return AffinitySaturationOfWords(field.Affinity)
}

/*
AffinitySaturationOfWords is the same normalization as AffinitySaturation for an
arbitrary affinity word slice (ingest routing, hypothetical merges).
*/
func AffinitySaturationOfWords(words []uint64) float64 {
	if len(words) == 0 {
		return 0
	}

	var popcount int

	for _, word := range words {
		popcount += bits.OnesCount64(word)
	}

	return float64(popcount) / 257.0
}

/*
SimulatedMergedAffinity returns the XOR aggregate MergeAffinity would produce
without mutating either operand. Empty existing behaves like the first merge
(copy incoming).
*/
func SimulatedMergedAffinity(existing []uint64, incoming []uint64) []uint64 {
	if len(incoming) == 0 {
		if len(existing) == 0 {
			return nil
		}

		out := make([]uint64, len(existing))
		copy(out, existing)

		return out
	}

	if len(existing) == 0 {
		out := make([]uint64, len(incoming))
		copy(out, incoming)

		return out
	}

	minLen := len(existing)
	if len(incoming) < minLen {
		minLen = len(incoming)
	}

	out := make([]uint64, len(existing))
	copy(out, existing)

	for wordIndex := 0; wordIndex < minLen; wordIndex++ {
		out[wordIndex] ^= incoming[wordIndex]
	}

	return out
}

/*
PredictAffinitySaturationAfterMerge reports AffinitySaturation after MergeAffinity
with valueAffinity, without modifying the field.
*/
func (field *Field) PredictAffinitySaturationAfterMerge(valueAffinity []uint64) float64 {
	if field == nil {
		return 0
	}

	merged := SimulatedMergedAffinity(field.Affinity, valueAffinity)

	return AffinitySaturationOfWords(merged)
}

/*
AffinityHammingDistance counts differing bits between two affinity word slices.
Extra words are XORed against zero (length mismatch still contributes distance).
*/
func AffinityHammingDistance(left []uint64, right []uint64) int {
	var distance int

	shared := len(left)
	if len(right) < shared {
		shared = len(right)
	}

	for wordIndex := 0; wordIndex < shared; wordIndex++ {
		distance += bits.OnesCount64(left[wordIndex] ^ right[wordIndex])
	}

	for wordIndex := shared; wordIndex < len(left); wordIndex++ {
		distance += bits.OnesCount64(left[wordIndex])
	}

	for wordIndex := shared; wordIndex < len(right); wordIndex++ {
		distance += bits.OnesCount64(right[wordIndex])
	}

	return distance
}

/*
NewCommunityField allocates an empty leaf Field used as an orchestrator
community bucket (Values + Affinity only; no phase-vector lanes).
*/
func NewCommunityField(modulus uint32) *Field {
	return &Field{
		modulus:      modulus,
		Fields:       nil,
		Cooccurrence: make(map[int]map[int]uint32),
		lastMode:     -1,
	}
}

/*
ResetLanes clears the current phase population while preserving the field shape.
It is used when the caller wants a fresh population snapshot for the next cycle.
*/
func (field *Field) ResetLanes() {
	if field == nil || len(field.Fields) == 0 {
		return
	}

	clear(field.Fields)
}

/*
Observe adds one weighted observation into the requested phase lane.
*/
func (field *Field) Observe(index int, weight uint32) {
	if field == nil || len(field.Fields) == 0 || weight == 0 {
		return
	}

	if index < 0 || index >= len(field.Fields) {
		return
	}

	lane := field.Fields[index]

	if lane == nil {
		lane = &Field{modulus: field.modulus}
		field.Fields[index] = lane
	}

	lane.amplitude = lane.amplitude + weight
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

func (field *Field) inverseMod(value uint32) uint32 {
	if value == 0 {
		return 0
	}

	result := uint64(1)
	basePow := uint64(value)
	exponent := uint64(field.modulus - 2)

	for exponent > 0 {
		if exponent%2 == 1 {
			result = (result * basePow) % uint64(field.modulus)
		}
		basePow = (basePow * basePow) % uint64(field.modulus)
		exponent /= 2
	}

	return uint32(result)
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
		childLane := child.Fields[laneIndex]

		if childLane == nil {
			continue
		}

		childValue := childLane.Dominant().Amplitude

		if childValue == 0 {
			continue
		}

		laneMultiplier := field.addMod(slotMultiplier, uint32(laneIndex+1))
		projected := field.mulMod(laneMultiplier, childValue)

		if field.Fields[laneIndex] == nil {
			field.Fields[laneIndex] = &Field{modulus: field.modulus}
		}

		field.Fields[laneIndex].amplitude = field.addMod(field.Fields[laneIndex].amplitude, projected)
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
		receiverLane := field.Fields[laneIndex]
		otherLane := other.Fields[laneIndex]

		var ampReceiver uint32
		var ampOther uint32

		if receiverLane != nil {
			ampReceiver = receiverLane.Dominant().Amplitude
		}

		if otherLane != nil {
			ampOther = otherLane.Dominant().Amplitude
		}

		product := field.mulMod(ampReceiver, ampOther)
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
