package geometry

import (
	"math/bits"
	"slices"

	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/data"
)

/*
GFRotation represents a composable affine transformation over GF(257):

	f(x) = (A·x + B) mod 257

where A ∈ {1..256} (non-zero, so the map is a bijection) and B ∈ {0..256}.

Composition is closed: if rot₁ = (a₁, b₁) and rot₂ = (a₂, b₂), then

	rot₂ ∘ rot₁ = ( a₂·a₁ mod 257,  a₂·b₁ + b₂ mod 257 )

This eliminates physical data movement entirely. Instead of shuffling
257 chords (16KB) per rotation, we compose two integers in O(1).
*/
type GFRotation struct {
	A uint16 // multiplicative coefficient, must be non-zero (1 for identity)
	B uint16 // additive offset
}

/*
RotationForChord maps a single chord to an injective GF(257) transform.
*/
func RotationForChord(c data.Chord) GFRotation {
	a, b := c.RotationSeed()

	return GFRotation{
		A: a,
		B: b,
	}
}

/*
IdentityRotation returns the identity transformation f(x) = x.
A=1, B=0; every face maps to itself. Used as the neutral element for composition.
*/
func IdentityRotation() GFRotation {
	return GFRotation{A: 1, B: 0}
}

/*
Forward maps a logical face index to its physical position under this rotation.
Computes (A·face + B) mod 257 with proper handling of negative modulo.
*/
func (rot GFRotation) Forward(face int) int {
	return (int(rot.A)*face + int(rot.B)%CubeFaces + CubeFaces) % CubeFaces
}

var inverseTable [257]int

func init() {
	for i := 1; i < 257; i++ {
		inverseTable[i] = ModInverse(i, CubeFaces)
	}
}

/*
Reverse maps a physical face index back to its logical byte value.
Computes the inverse affine transform: finds A⁻¹ in GF(257) then (face - B)·A⁻¹ mod 257.
*/
func (rot GFRotation) Reverse(face int) int {
	invA := inverseTable[int(rot.A)]
	val := (face - int(rot.B)) % CubeFaces

	if val < 0 {
		val += CubeFaces
	}

	return (invA * val) % CubeFaces
}

/*
Compose returns the rotation equivalent to applying r first, then other.
other(r(x)) = other.A·(r.A·x + r.B) + other.B = (other.A·r.A)·x + (other.A·r.B + other.B).
Enables O(1) composition without data movement.
*/
func (rot GFRotation) Compose(other GFRotation) GFRotation {
	return GFRotation{
		A: uint16((int(other.A) * int(rot.A)) % CubeFaces),
		B: uint16((int(other.A)*int(rot.B) + int(other.B)) % CubeFaces),
	}
}

/*
Inverse returns the affine inverse so rot.Inverse().Compose(rot) is identity.
*/
func (rot GFRotation) Inverse() GFRotation {
	invA := inverseTable[int(rot.A)]
	b := (-invA * int(rot.B)) % CubeFaces

	if b < 0 {
		b += CubeFaces
	}

	return GFRotation{A: uint16(invA), B: uint16(b)}
}

/*
ApplyToChord maps every active prime index in the logical 257-face width through
this affine rotation.
*/
func (rot GFRotation) ApplyToChord(chord data.Chord) data.Chord {
	if chord.ActiveCount() == 0 {
		return chord
	}

	var out data.Chord

	for blockIdx := range config.ChordBlocks {
		block := chord[blockIdx]

		for block != 0 {
			bitIdx := bits.TrailingZeros64(block)
			primeIdx := blockIdx*64 + bitIdx

			if primeIdx < CubeFaces {
				out.Set(rot.Forward(primeIdx))
			}

			block &= block - 1
		}
	}

	return out
}

/*
ReverseChord maps every active prime index in the chord through the inverse of
this affine rotation.
*/
func (rot GFRotation) ReverseChord(chord data.Chord) data.Chord {
	if chord.ActiveCount() == 0 {
		return chord
	}

	var out data.Chord

	for blockIdx := range config.ChordBlocks {
		block := chord[blockIdx]

		for block != 0 {
			bitIdx := bits.TrailingZeros64(block)
			primeIdx := blockIdx*64 + bitIdx

			if primeIdx < CubeFaces {
				out.Set(rot.Reverse(primeIdx))
			}

			block &= block - 1
		}
	}

	return out
}

/*
StateChord deterministically projects the affine state (A, B) into the same
257-face chord space as ordinary data.
*/
func (rot GFRotation) StateChord() data.Chord {
	aByte := byte((int(rot.A) + 255) % 256)
	bByte := byte(int(rot.B) % 256)
	mixByte := byte((int(rot.A) + int(rot.B)) % 256)

	aBase := data.BaseChord(aByte)
	bBase := data.BaseChord(bByte)
	mixBase := data.BaseChord(mixByte)

	aChord := aBase.RollLeft(int(rot.B) % CubeFaces)
	bChord := bBase.RollLeft(int(rot.A) % CubeFaces)
	mixChord := mixBase.RollLeft((int(rot.A) + int(rot.B)*3) % CubeFaces)

	state := data.ChordOR(&aChord, &bChord)

	return data.ChordOR(&state, &mixChord)
}

/*
ModInverse computes a⁻¹ mod m using the extended Euclidean algorithm.
*/
func ModInverse(a, m int) int {
	or, r := a, m
	os, s := 1, 0

	for r != 0 {
		q := or / r
		or, r = r, or-q*r
		os, s = s, os-q*s
	}

	res := (os % m)
	if res < 0 {
		res += m
	}

	return res
}

/*
RotationTable holds the 9 core topological transformations derived from a generator.
*/
type RotationTable struct {
	X90, X180, X270 GFRotation
	Y90, Y180, Y270 GFRotation
	Z90, Z180, Z270 GFRotation
}

/*
BuildRotTable computes the geometric operators from a given GF(257) generator 'g'.
X operations are additive translations. Y and Z are multiplicative dilations and affine combos.
*/
func BuildRotTable(g uint16) RotationTable {
	gi := uint16(ModInverse(int(g), CubeFaces))
	g2 := (g * g) % CubeFaces

	return RotationTable{
		X90:  GFRotation{A: 1, B: 1},
		X180: GFRotation{A: 1, B: 2},
		X270: GFRotation{A: 1, B: 256},

		Y90:  GFRotation{A: g, B: 0},
		Y180: GFRotation{A: g2, B: 0},
		Y270: GFRotation{A: gi, B: 0},

		Z90:  GFRotation{A: g, B: 1},
		Z180: GFRotation{A: g2, B: (g + 1) % CubeFaces},
		Z270: GFRotation{A: gi, B: (CubeFaces - gi) % CubeFaces},
	}
}

var DefaultRotTable = BuildRotTable(3)

/*
RotationEvent maps a canonical GF(257) micro-rotation back to the topological event.
Returns (event, true) if rot matches X90/Y90/Z90 or XX; (0, false) otherwise.
*/
func RotationEvent(rot GFRotation) (int, bool) {
	switch rot {
	case DefaultRotTable.X90:
		return EventDensitySpike, true
	case DefaultRotTable.Y90:
		return EventPhaseInversion, true
	case DefaultRotTable.Z90:
		return EventDensityTrough, true
	case DefaultRotTable.X180:
		return EventLowVarianceFlux, true
	default:
		return 0, false
	}
}

/*
EventRotation returns the GFRotation corresponding to a topological event.
EventDensitySpike→X, EventPhaseInversion→Y, EventDensityTrough→Z, EventLowVarianceFlux→XX.
Unknown events return IdentityRotation.
*/
func EventRotation(event int) GFRotation {
	switch event {
	case EventDensitySpike:
		return DefaultRotTable.X90
	case EventPhaseInversion:
		return DefaultRotTable.Y90
	case EventDensityTrough:
		return DefaultRotTable.Z90
	case EventLowVarianceFlux:
		return DefaultRotTable.X180
	default:
		return IdentityRotation()
	}
}

/*
ComposeEvents composes all event rotations in sequence.
Returns a single GFRotation equivalent to applying each EventRotation in order.
*/
func ComposeEvents(events []int) GFRotation {
	rot := IdentityRotation()

	for ev := range slices.Values(events) {
		rot = rot.Compose(EventRotation(ev))
	}

	return rot
}
