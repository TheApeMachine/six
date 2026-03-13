package numeric

import (
	"math"

	config "github.com/theapemachine/six/pkg/core"
)

/*
Prime holds the first NBasis primes, indexed 0..NBasis-1.
Used as omega frequencies for PhaseDial and structural indices for chord encoding.
*/
type Prime struct {
	Basis []int32
}

var prime *Prime
var Primes []int32

/*
init populates Primes with the first NBasis primes via SieveOfEratosthenes.
*/
func init() {
	prime = New()
	Primes = append([]int32(nil), prime.Basis...)
}

// New creates and returns a *Prime initialized with a Basis slice and populated via SieveOfEratosthenes.
func New() *Prime {
	prime := &Prime{
		Basis: make([]int32, config.Numeric.NBasis),
	}

	prime.SieveOfEratosthenes(sieveUpperBound(
		config.Numeric.NBasis,
	))
	return prime
}

/*
sieveUpperBound returns the upper limit for the sieve so the first nBasis primes are captured.
Uses Dusart bound: n·(ln n + ln ln n) with floor 20 and +50 margin.
*/
func sieveUpperBound(nBasis int) int {
	if nBasis <= 1 {
		return 70
	}
	lnN := math.Log(float64(nBasis))

	return int(math.Ceil(
		math.Max(20, float64(nBasis)*(lnN+math.Log(lnN))),
	)) + 50
}

/*
SieveOfEratosthenes fills Basis with the first NBasis primes from [2..n).
Marks multiples of each prime and collects unmarked integers in order.
*/
func (prime *Prime) SieveOfEratosthenes(limit int) {
	checked := make([]bool, limit)
	sqrtLimit := int(math.Sqrt(float64(limit)))

	for candidate := 2; candidate <= sqrtLimit; candidate++ {
		if !checked[candidate] {
			for multiple := candidate * candidate; multiple < limit; multiple += candidate {
				checked[multiple] = true
			}
		}
	}

	idx := 0

	for candidate := 2; candidate < limit && idx < config.Numeric.NBasis; candidate++ {
		if !checked[candidate] {
			prime.Basis[idx] = int32(candidate)
			idx++
		}
	}
}
