package numeric

import (
	"math"

	config "github.com/theapemachine/six/core"
)

type Prime struct {
	Basis []int32
}

var prime *Prime
var Primes []int32

func init() {
	prime = New()
	Primes = prime.Basis
}

func New() *Prime {
	prime := &Prime{
		Basis: make([]int32, config.Numeric.NBasis),
	}

	prime.SieveOfEratosthenes(sieveUpperBound(
		config.Numeric.NBasis,
	))
	return prime
}

func sieveUpperBound(nBasis int) int {
	lnN := math.Log(float64(nBasis))

	return int(math.Ceil(
		math.Max(20, float64(nBasis)*(lnN+math.Log(lnN))),
	)) + 50 // small safety margin
}

func (prime *Prime) SieveOfEratosthenes(n int) {
	checked := make([]bool, n)
	sqrt_n := int(math.Sqrt(float64(n)))

	for i := 2; i <= sqrt_n; i++ {
		if !checked[i] {
			for j := i * i; j < n; j += i {
				checked[j] = true
			}
		}
	}

	idx := 0

	for i := 2; i < n && idx < config.Numeric.NBasis; i++ {
		if !checked[i] {
			prime.Basis[idx] = int32(i)
			idx++
		}
	}
}
