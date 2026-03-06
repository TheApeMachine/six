package resonance

import (
	"math/bits"

	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/data"
)

// PositionalPrimeStart is the first prime index reserved for positional encoding.
// When >= 0, ApplyPositionalShift sets chord bit at PositionalPrimeStart + (pos % PositionSlots).
// When < 0 (default), no positional encoding (pure semantic matching).
var PositionalPrimeStart = -1

// PositionSlots is the number of distinct positions encodable when positional encoding is active.
const PositionSlots = 64

/*
ApplyPositionalShift encodes position into the chord (for positional encoding).
Pure semantic matching uses a no-op; positional encoding multiplies by BasisPrimes.
When PositionalPrimeStart >= 0, sets the bit at BasisPrimes[PositionalPrimeStart + pos mod PositionSlots].
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
FillScore measures how well a candidate chord fills a structural hole.
Returns a value in [0, 1] where 1 = perfect fill with no extra primes.
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
TransitiveResonance executes a structural analogy (A:B :: C:D) using pure bitwise logic.
Given F1(A,B), F2(C,B), and F3(C,D), it returns the hypothesis H(A,D).

Example: "Cat wants food" + "Dog wants food" + "Dog is animal" → "Cat is animal"
  - B = shared context (wants food)
  - C = shared subject (dog)
  - A = F1 without B (cat)
  - D = F3 without C (is animal)
  - H = A | D (cat is animal)

No neural network required — the prime substrate performs symbolic logic natively.
*/
func TransitiveResonance(f1, f2, f3 *data.Chord) data.Chord {
	B := data.ChordGCD(f1, f2) // Shared context ("wormhole" = bitwise intersection: f1 & f2)
	C := data.ChordGCD(f2, f3) // Shared subject ("wormhole" = bitwise intersection: f2 & f3)

	A := data.ChordHole(f1, &B) // F1 without B
	D := data.ChordHole(f3, &C) // F3 without C

	bA := A
	bD := D

	return data.ChordOR(&bA, &bD) // Forged hypothesis (A ∪ D)
}

/*
popcount counts the number of 1-bits in a uint64
*/
func popcount(x uint64) (count int) {
	return bits.OnesCount64(x)
}
