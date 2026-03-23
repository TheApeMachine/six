package cpu

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
MobiusSign returns μ(v) = (-1)^k where k is the number of active primes.
For square-free Values on the prime lattice this is the exact Möbius function:
even popcount → +1, odd popcount → -1.
*/
func MobiusSign(value *primitive.Value) int {
	if bits.OnesCount64(value[0])%2 == 0 {
		return 1
	}

	return -1
}

/*
ActiveBits collects the positions of all set bits in the core field,
ordered from lowest to highest. Callers must ensure PopCount is small
enough that full subset enumeration over the result is tractable.
*/
func ActiveBits(value *primitive.Value) []int {
	positions := make([]int, 0, bits.OnesCount64(value[0]))

	for idx := range primitive.CoreBits {
		if value[0]&(1<<idx) != 0 {
			positions = append(positions, idx)
		}
	}

	return positions
}

/*
SubsetValue constructs a Value whose active bits are the positions
selected by the bitmask. Bit i of mask selects positions[i].
*/
func SubsetValue(positions []int, mask uint64) *primitive.Value {
	subset := primitive.NewValue()

	for idx, pos := range positions {
		if mask&(1<<idx) != 0 {
			subset[0] |= 1 << pos
		}
	}

	return subset
}

/*
Quotient returns the lattice quotient n/d on the square-free lattice,
which is the set difference of active bits: n AND NOT d.
Requires d|n (d's bits are a subset of n's bits).
*/
func Quotient(numerator, divisor *primitive.Value) *primitive.Value {
	quotient := primitive.NewValue()
	for i := range primitive.Words {
		quotient[i] = numerator[i] &^ divisor[i]
	}
	quotient[primitive.Words-1] &= primitive.LastMask

	return quotient
}

/*
Divides reports whether divisor divides numerator on the square-free
lattice: every active bit in divisor is also active in numerator.
Equivalent to AND(divisor, numerator) == divisor.
*/
func Divides(divisor, numerator *primitive.Value) bool {
	for idx := range primitive.Words {
		if divisor[idx]&numerator[idx] != divisor[idx] {
			return false
		}
	}

	return true
}

/*
MobiusInvert applies the Möbius inversion formula on the square-free
Value lattice:

	f(n) = Σ_{d|n} μ(n/d) · g(d)

where d|n means d's active bits are a subset of n's active bits.
g is the aggregate function being inverted. The function enumerates all
2^k divisors of target (where k = target.PopCount()), so target must
have a manageable number of active bits (≤ ~20).

This is the algebraically exact inverse of summation over divisors.
If g was formed by g(n) = Σ_{d|n} f(d), then MobiusInvert recovers f(n).
*/
func MobiusInvert(target *primitive.Value, aggregate func(*primitive.Value) int) int {
	positions := ActiveBits(target)
	numBits := uint64(len(positions))
	result := 0

	for mask := uint64(0); mask < (1 << numBits); mask++ {
		divisor := SubsetValue(positions, mask)
		quotient := Quotient(target, divisor)
		sign := MobiusSign(quotient)

		result += sign * aggregate(divisor)
	}

	return result
}

/*
ContributorCounter builds the aggregate function g for a set of known
contributors. g(d) counts how many contributors divide d (i.e., have
all their active bits present in d). This is what the fold graph
naturally tracks during OR accumulation.
*/
func ContributorCounter(contributors []*primitive.Value) func(*primitive.Value) int {
	return func(query *primitive.Value) int {
		count := 0

		for _, contributor := range contributors {
			if Divides(contributor, query) {
				count++
			}
		}

		return count
	}
}
