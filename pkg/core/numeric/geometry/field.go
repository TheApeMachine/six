package geometry

import "math"

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
	modulus uint32
	lanes   []uint32
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
		return &Field{modulus: modulus, lanes: nil}
	}

	return &Field{
		modulus: modulus,
		lanes:   make([]uint32, laneCount),
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
LaneLen returns the number of lanes (ring width for Alignment).
*/
func (field *Field) LaneLen() int {
	if field == nil {
		return 0
	}

	return len(field.lanes)
}

/*
Rotate applies a whole-vector affine phase rotation: each lane i maps
v ↦ a·v + (b + i + 1) in GF(modulus), matching the prior per-tier behavior.
*/
func (field *Field) Rotate(multiplier uint32, bias uint32) {
	if field == nil {
		return
	}

	for laneIndex := range field.lanes {
		laneBias := field.addMod(bias, uint32(laneIndex+1))
		field.lanes[laneIndex] = field.affineMod(field.lanes[laneIndex], multiplier, laneBias)
	}
}

/*
ObserveByte injects one byte at position into the field: identity, orbit, and
mirror lanes use the same layout at every modulus.
*/
func (field *Field) ObserveByte(token byte, position int) {
	if field == nil || len(field.lanes) == 0 {
		return
	}

	width := len(field.lanes)
	positionMod := positiveMod(position, width)

	multiplier := field.reduceU32(uint32(positionMod + 1))

	if multiplier == 0 {
		multiplier = 1
	}

	bias := field.addMod(uint32(token), 1)
	identityLane := positiveMod(int(token), width)
	orbitLane := positiveMod(identityLane+position+1, width)
	mirrorLane := positiveMod(identityLane+positionMod+width/2, width)

	field.lanes[identityLane] = field.affineMod(field.lanes[identityLane], multiplier, bias)
	field.lanes[orbitLane] = field.addMod(field.lanes[orbitLane], bias)
	field.lanes[mirrorLane] = field.addMod(field.lanes[mirrorLane], multiplier)
}

/*
ObserveBytes folds a full byte sequence into the phase vector.
*/
func (field *Field) ObserveBytes(data []byte) {
	if field == nil {
		return
	}

	for position, token := range data {
		field.ObserveByte(token, position)
	}
}

/*
Dominant returns the strongest lane and mass-normalized concentration.
*/
func (field *Field) Dominant() PhaseMode {
	if field == nil || len(field.lanes) == 0 {
		return PhaseMode{Index: -1}
	}

	var totalMass uint64

	bestIndex := -1

	var bestSignal uint32

	for laneIndex, laneValue := range field.lanes {
		totalMass += uint64(laneValue)

		if laneValue > bestSignal {
			bestIndex = laneIndex
			bestSignal = laneValue
		}
	}

	if totalMass == 0 {
		return PhaseMode{Index: -1}
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

	return LaneRingAlignment(leftIndex, rightIndex, len(field.lanes))
}

func (field *Field) addMod(left uint32, right uint32) uint32 {
	sum := uint64(left) + uint64(right)

	return field.reduceU64(sum)
}

func (field *Field) mulMod(left uint32, right uint32) uint32 {
	prod := uint64(left) * uint64(right)

	return field.reduceU64(prod)
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
reduce257Uint32 maps a uint32 into GF(257) via reduce257Uint64.
*/
func reduce257Uint32(value uint32) uint16 {
	return uint16(reduce257Uint64(uint64(value)))
}

/*
reduce257Uint64 exploits 256 ≡ −1 (mod 257): fold all eight bytes with
alternating sign so bits 0–63 participate in the residue.
*/
func reduce257Uint64(value uint64) uint32 {
	var acc int64

	for byteIdx := 0; byteIdx < 8; byteIdx++ {
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
reduce8191Uint32 maps a uint32 into GF(8191) via reduce8191Uint64.
*/
func reduce8191Uint32(value uint32) uint16 {
	return uint16(reduce8191Uint64(uint64(value)))
}

/*
reduce8191Uint64 exploits 2^13 ≡ 1 (mod 8191): fold the full uint64 with
the same low/high 13-bit split until the residue fits.
*/
func reduce8191Uint64(value uint64) uint32 {
	const mask13 = uint64(0x1FFF)

	reduced := value

	for reduced >= uint64(Mod8191) {
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

	parentLanes := len(field.lanes)
	childLanes := len(child.lanes)

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

	for laneIndex := 0; laneIndex < childLanes; laneIndex++ {
		childValue := child.lanes[laneIndex]

		if childValue == 0 {
			continue
		}

		laneMultiplier := field.addMod(slotMultiplier, uint32(laneIndex+1))
		projected := field.mulMod(laneMultiplier, childValue)
		field.lanes[laneIndex] = field.addMod(field.lanes[laneIndex], projected)
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

	if field.modulus != other.modulus || len(field.lanes) != len(other.lanes) {
		return 0
	}

	var accumulator uint32

	for laneIndex := range field.lanes {
		product := field.mulMod(field.lanes[laneIndex], other.lanes[laneIndex])
		accumulator = field.addMod(accumulator, product)
	}

	return accumulator
}

/*
LiftFromBytes builds a fresh field of the given modulus from raw bytes via
ObserveBytes — same entry path at any tier that ingests octet streams.
*/
func LiftFromBytes(data []byte, modulus uint32) *Field {
	field := NewField(modulus)
	field.ObserveBytes(data)

	return field
}

/*
DominantForBytes is LiftFromBytes(data, Mod257).Dominant() — octet ingress at
the lowest GF layer.
*/
func DominantForBytes(data []byte) PhaseMode {
	return LiftFromBytes(data, Mod257).Dominant()
}

/*
Clone returns an independent copy of the field (same modulus and lanes).
*/
func (field *Field) Clone() *Field {
	if field == nil {
		return nil
	}

	out := newFieldLanes(len(field.lanes), field.modulus)
	copy(out.lanes, field.lanes)

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
