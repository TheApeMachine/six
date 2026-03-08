package resonance

import (
	"math/bits"

	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/data"
)

/*
PositionalPrimeStart is the first prime index for positional encoding. When >= 0,
ApplyPositionalShift sets chord bit at PositionalPrimeStart + (pos % PositionSlots).
When < 0 (default), no positional encoding (pure semantic matching).
*/
var PositionalPrimeStart = -1

/*
PositionSlots is the number of distinct position slots (pos mod PositionSlots).
*/
const PositionSlots = 64

/*
ApplyPositionalShift sets the chord bit at index PositionalPrimeStart + (pos % PositionSlots)
when PositionalPrimeStart >= 0. Otherwise no-op.
*/
func ApplyPositionalShift(chord *data.Chord, pos int) {
	if PositionalPrimeStart < 0 {
		return
	}
	primeIdx := PositionalPrimeStart + (pos % PositionSlots)
	if primeIdx >= config.Numeric.NBasis {
		return
	}
	chord.Set(primeIdx)
}

/*
FillScore measures how well candidate covers hole (ChordHole target/existing).
Score = filled / (needed * (1 + extra)); 1.0 when hole fully covered with no excess primes.
*/
func FillScore(hole, candidate *data.Chord) (score float64) {
	needed := 0
	filled := 0
	extra := 0

	for i := range config.ChordBlocks {
		h := hole[i]
		c := candidate[i]

		needed += popcount(h)
		filled += popcount(h & c)
		extra += popcount(c &^ h) // primes in candidate that aren't in hole
	}

	if needed == 0 {
		return 0
	}

	return float64(filled) / (float64(needed) * (1.0 + float64(extra)))
}

/*
TransitiveResonance computes structural analogy (A:B :: C:D) via chord GCD/Hole.
B = GCD(f1,f2), C = GCD(f2,f3), A = Hole(f1,B), D = Hole(f3,C). Returns A|D.
Example: f1=cat+food, f2=dog+food, f3=dog+animal → A=cat, D=animal → cat|animal.
*/
func TransitiveResonance(f1, f2, f3 *data.Chord) data.Chord {
	B := data.ChordGCD(f1, f2) // Shared context
	C := data.ChordGCD(f2, f3) // Shared subject

	A := data.ChordHole(f1, &B) // F1 without B
	D := data.ChordHole(f3, &C) // F3 without C

	bA := A
	bD := D

	return data.ChordOR(&bA, &bD)
}

/*
popcount returns the number of set bits in x.
*/
func popcount(x uint64) (count int) {
	return bits.OnesCount64(x)
}
