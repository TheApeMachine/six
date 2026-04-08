package gf

import "math"

/*
PhaseWidth is the shared lane count for trie, node, and global phase
vectors. One lane per byte keeps the hierarchy sympathetic to the
existing byte-oriented MarkovTrie path while still letting higher
layers live in larger finite fields.
*/
const PhaseWidth = 256

/*
The project's three field moduli.
*/
const (
	Mod257   = 257
	Mod8191  = 8191
	Mod65537 = 65537
)

/*
DominantPhase is the strongest occupied lane in a phase vector.
Concentration is peak / total mass, giving a scale-free measure of how
collapsed the vector is around one phase.
*/
type DominantPhase struct {
	Index         int
	Amplitude     uint32
	Concentration float64
}

/*
Vector257 is the trie-local phase state over GF(257).
*/
type Vector257 [PhaseWidth]uint16

/*
Vector8191 is the node phase state over GF(8191).
*/
type Vector8191 [PhaseWidth]uint16

/*
Vector65537 is the mesh-global phase state over GF(65537).
*/
type Vector65537 [PhaseWidth]uint32

/*
NewVector257 allocates a zeroed trie phase vector.
*/
func NewVector257() *Vector257 {
	return &Vector257{}
}

/*
NewVector8191 allocates a zeroed node phase vector.
*/
func NewVector8191() *Vector8191 {
	return &Vector8191{}
}

/*
NewVector65537 allocates a zeroed global phase vector.
*/
func NewVector65537() *Vector65537 {
	return &Vector65537{}
}

/*
Clone copies the trie phase vector.
*/
func (vector *Vector257) Clone() *Vector257 {
	if vector == nil {
		return NewVector257()
	}

	clone := *vector

	return &clone
}

/*
Clone copies the node phase vector.
*/
func (vector *Vector8191) Clone() *Vector8191 {
	if vector == nil {
		return NewVector8191()
	}

	clone := *vector

	return &clone
}

/*
Clone copies the global phase vector.
*/
func (vector *Vector65537) Clone() *Vector65537 {
	if vector == nil {
		return NewVector65537()
	}

	clone := *vector

	return &clone
}

/*
LiftBytes builds a trie-scale phase vector from raw bytes.
*/
func LiftBytes(data []byte) *Vector257 {
	vector := NewVector257()
	vector.ObserveBytes(data)

	return vector
}

/*
DominantForBytes extracts the strongest byte-phase from raw bytes.
*/
func DominantForBytes(data []byte) DominantPhase {
	return LiftBytes(data).Dominant()
}

/*
Alignment scores how closely two dominant lanes line up on the shared
256-lane ring. 1 is perfect alignment, 0 is half a rotation away.
*/
func Alignment(leftIndex int, rightIndex int) float64 {
	if leftIndex < 0 || rightIndex < 0 {
		return 0
	}

	distance := leftIndex - rightIndex

	if distance < 0 {
		distance = -distance
	}

	if distance > PhaseWidth/2 {
		distance = PhaseWidth - distance
	}

	return 1.0 - (float64(distance) / float64(PhaseWidth/2))
}

/*
InterferenceMultiplier turns phase alignment and signal strength into an
exponential beam/training bias. Fully aligned, high-gain candidates
receive a large constructive boost; anti-phase candidates are damped.
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
Gain257 normalizes a GF(257) interference residue into [0,1].
*/
func Gain257(value uint16) float64 {
	return float64(value) / float64(Mod257-1)
}

/*
Reduce257 exploits 2^8 = -1 (mod 257).
*/
func Reduce257(value uint32) uint16 {
	reduced := int32(value&0xff) - int32(value>>8)

	for reduced < 0 {
		reduced += Mod257
	}

	for reduced >= Mod257 {
		reduced -= Mod257
	}

	return uint16(reduced)
}

/*
Reduce8191 exploits 2^13 = 1 (mod 8191).
*/
func Reduce8191(value uint32) uint16 {
	reduced := value

	for reduced >= Mod8191 {
		if reduced == Mod8191 {
			return 0
		}

		reduced = (reduced & Mod8191) + (reduced >> 13)
	}

	return uint16(reduced)
}

/*
Reduce65537 exploits 2^16 = -1 (mod 65537).
*/
func Reduce65537(value uint64) uint32 {
	reduced := int64(value&0xffff) - int64(value>>16)

	for reduced < 0 || reduced >= Mod65537 {
		if reduced < 0 {
			reduced += Mod65537
			continue
		}

		reduced = int64(uint64(reduced)&0xffff) - int64(uint64(reduced)>>16)
	}

	return uint32(reduced)
}

/*
Add257 adds two GF(257) elements.
*/
func Add257(leftValue uint16, rightValue uint16) uint16 {
	return Reduce257(uint32(leftValue) + uint32(rightValue))
}

/*
Mul257 multiplies two GF(257) elements.
*/
func Mul257(leftValue uint16, rightValue uint16) uint16 {
	return Reduce257(uint32(leftValue) * uint32(rightValue))
}

/*
Affine257 applies v' = a*v + b in GF(257).
*/
func Affine257(value uint16, multiplier uint16, bias uint16) uint16 {
	return Add257(Mul257(value, multiplier), bias)
}

/*
Add8191 adds two GF(8191) elements.
*/
func Add8191(leftValue uint16, rightValue uint16) uint16 {
	return Reduce8191(uint32(leftValue) + uint32(rightValue))
}

/*
Mul8191 multiplies two GF(8191) elements.
*/
func Mul8191(leftValue uint16, rightValue uint16) uint16 {
	return Reduce8191(uint32(leftValue) * uint32(rightValue))
}

/*
Affine8191 applies v' = a*v + b in GF(8191).
*/
func Affine8191(value uint16, multiplier uint16, bias uint16) uint16 {
	return Add8191(Mul8191(value, multiplier), bias)
}

/*
Add65537 adds two GF(65537) elements.
*/
func Add65537(leftValue uint32, rightValue uint32) uint32 {
	return Reduce65537(uint64(leftValue) + uint64(rightValue))
}

/*
Mul65537 multiplies two GF(65537) elements.
*/
func Mul65537(leftValue uint32, rightValue uint32) uint32 {
	return Reduce65537(uint64(leftValue) * uint64(rightValue))
}

/*
Affine65537 applies v' = a*v + b in GF(65537).
*/
func Affine65537(value uint32, multiplier uint32, bias uint32) uint32 {
	return Add65537(Mul65537(value, multiplier), bias)
}

/*
Rotate applies a whole-vector affine phase rotation in GF(257).
Lane index contributes to the bias so a broadcast does not collapse all
lanes onto the same residue.
*/
func (vector *Vector257) Rotate(multiplier uint16, bias uint16) {
	if vector == nil {
		return
	}

	for laneIndex := range PhaseWidth {
		laneBias := Add257(bias, uint16(laneIndex+1))
		(*vector)[laneIndex] = Affine257(
			(*vector)[laneIndex],
			multiplier,
			laneBias,
		)
	}
}

/*
Rotate applies a whole-vector affine phase rotation in GF(8191).
*/
func (vector *Vector8191) Rotate(multiplier uint16, bias uint16) {
	if vector == nil {
		return
	}

	for laneIndex := range PhaseWidth {
		laneBias := Add8191(bias, uint16(laneIndex+1))
		(*vector)[laneIndex] = Affine8191(
			(*vector)[laneIndex],
			multiplier,
			laneBias,
		)
	}
}

/*
Rotate applies a whole-vector affine phase rotation in GF(65537).
*/
func (vector *Vector65537) Rotate(multiplier uint32, bias uint32) {
	if vector == nil {
		return
	}

	for laneIndex := range PhaseWidth {
		laneBias := Add65537(bias, uint32(laneIndex+1))
		(*vector)[laneIndex] = Affine65537(
			(*vector)[laneIndex],
			multiplier,
			laneBias,
		)
	}
}

/*
ObserveByte injects one byte into the trie-local phase vector. The byte's
identity, position, and orbit lane all contribute so the phase captures
sequence layout rather than reducing to a plain histogram.
*/
func (vector *Vector257) ObserveByte(token byte, position int) {
	if vector == nil {
		return
	}

	multiplier := Reduce257(uint32((position % PhaseWidth) + 1))

	if multiplier == 0 {
		multiplier = 1
	}

	bias := Add257(uint16(token), 1)
	identityLane := int(token)
	orbitLane := (identityLane + position + 1) & (PhaseWidth - 1)
	mirrorLane := identityLane ^ (position & (PhaseWidth - 1))

	(*vector)[identityLane] = Affine257(
		(*vector)[identityLane],
		multiplier,
		bias,
	)

	(*vector)[orbitLane] = Add257(
		(*vector)[orbitLane],
		bias,
	)

	(*vector)[mirrorLane] = Add257(
		(*vector)[mirrorLane],
		multiplier,
	)
}

/*
ObserveBytes folds an entire byte sequence into the trie-local phase.
*/
func (vector *Vector257) ObserveBytes(data []byte) {
	if vector == nil {
		return
	}

	for position, token := range data {
		vector.ObserveByte(token, position)
	}
}

/*
Dot computes GF(257) interference between two trie vectors.
*/
func (vector *Vector257) Dot(other *Vector257) uint16 {
	if vector == nil || other == nil {
		return 0
	}

	accumulator := uint16(0)

	for laneIndex := range PhaseWidth {
		product := Mul257((*vector)[laneIndex], (*other)[laneIndex])
		accumulator = Add257(accumulator, product)
	}

	return accumulator
}

/*
Dot computes GF(8191) interference between two node vectors.
*/
func (vector *Vector8191) Dot(other *Vector8191) uint16 {
	if vector == nil || other == nil {
		return 0
	}

	accumulator := uint16(0)

	for laneIndex := range PhaseWidth {
		product := Mul8191((*vector)[laneIndex], (*other)[laneIndex])
		accumulator = Add8191(accumulator, product)
	}

	return accumulator
}

/*
Dot computes GF(65537) interference between two global vectors.
*/
func (vector *Vector65537) Dot(other *Vector65537) uint32 {
	if vector == nil || other == nil {
		return 0
	}

	accumulator := uint32(0)

	for laneIndex := range PhaseWidth {
		product := Mul65537((*vector)[laneIndex], (*other)[laneIndex])
		accumulator = Add65537(accumulator, product)
	}

	return accumulator
}

/*
Dominant extracts the strongest trie-local phase lane.
*/
func (vector Vector257) Dominant() DominantPhase {
	var (
		totalMass  uint64
		bestIndex  = -1
		bestSignal uint32
	)

	for laneIndex := range PhaseWidth {
		laneValue := uint32(vector[laneIndex])

		totalMass += uint64(laneValue)

		if laneValue > bestSignal {
			bestIndex = laneIndex
			bestSignal = laneValue
		}
	}

	if totalMass == 0 {
		return DominantPhase{Index: -1}
	}

	return DominantPhase{
		Index:         bestIndex,
		Amplitude:     bestSignal,
		Concentration: float64(bestSignal) / float64(totalMass),
	}
}

/*
Dominant extracts the strongest node phase lane.
*/
func (vector Vector8191) Dominant() DominantPhase {
	var (
		totalMass  uint64
		bestIndex  = -1
		bestSignal uint32
	)

	for laneIndex := range PhaseWidth {
		laneValue := uint32(vector[laneIndex])

		totalMass += uint64(laneValue)

		if laneValue > bestSignal {
			bestIndex = laneIndex
			bestSignal = laneValue
		}
	}

	if totalMass == 0 {
		return DominantPhase{Index: -1}
	}

	return DominantPhase{
		Index:         bestIndex,
		Amplitude:     bestSignal,
		Concentration: float64(bestSignal) / float64(totalMass),
	}
}

/*
Dominant extracts the strongest global phase lane.
*/
func (vector Vector65537) Dominant() DominantPhase {
	var (
		totalMass  uint64
		bestIndex  = -1
		bestSignal uint32
	)

	for laneIndex := range PhaseWidth {
		laneValue := vector[laneIndex]

		totalMass += uint64(laneValue)

		if laneValue > bestSignal {
			bestIndex = laneIndex
			bestSignal = laneValue
		}
	}

	if totalMass == 0 {
		return DominantPhase{Index: -1}
	}

	return DominantPhase{
		Index:         bestIndex,
		Amplitude:     bestSignal,
		Concentration: float64(bestSignal) / float64(totalMass),
	}
}

/*
AccumulateProjected257 lifts a trie vector into node phase space and
adds it into the receiver. slot differentiates otherwise identical
local trie phases so the node phase preserves regional composition.
*/
func (vector *Vector8191) AccumulateProjected257(localPhase *Vector257, slot int) {
	if vector == nil || localPhase == nil {
		return
	}

	slotMultiplier := Reduce8191(uint32((slot % PhaseWidth) + 1))

	if slotMultiplier == 0 {
		slotMultiplier = 1
	}

	for laneIndex := range PhaseWidth {
		localValue := localPhase[laneIndex]

		if localValue == 0 {
			continue
		}

		laneMultiplier := Add8191(slotMultiplier, uint16(laneIndex+1))
		projected := Mul8191(laneMultiplier, uint16(localValue))

		(*vector)[laneIndex] = Add8191(
			(*vector)[laneIndex],
			projected,
		)
	}
}

/*
AccumulateProjected8191 lifts a node vector into global phase space and
adds it into the receiver. slot differentiates incoming peers.
*/
func (vector *Vector65537) AccumulateProjected8191(nodePhase *Vector8191, slot int) {
	if vector == nil || nodePhase == nil {
		return
	}

	slotMultiplier := Reduce65537(uint64((slot % PhaseWidth) + 1))

	if slotMultiplier == 0 {
		slotMultiplier = 1
	}

	for laneIndex := range PhaseWidth {
		nodeValue := uint32(nodePhase[laneIndex])

		if nodeValue == 0 {
			continue
		}

		laneMultiplier := Add65537(slotMultiplier, uint32(laneIndex+1))
		projected := Mul65537(laneMultiplier, nodeValue)

		(*vector)[laneIndex] = Add65537(
			(*vector)[laneIndex],
			projected,
		)
	}
}
