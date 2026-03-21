package numeric

/*
Phase is a value in the GF(8191) field, representing a state, rotation, or semantic anchor.
*/
type Phase uint32

const (
	FieldPrime  uint32 = 8191
	InverseBase uint32 = 8189

	CoreBits    = 8191
	CoreBlocks  = (CoreBits + 63) / 64
	ShellBlocks = 3
	TotalBlocks = CoreBlocks + ShellBlocks

	mersenneShift = 13
	mersenneMask  = uint32(FieldPrime)
)

/*
FieldPrimitive is the smallest primitive root of GF(8191), derived at init by
exhaustive verification against all prime factors of p−1. A primitive root g
generates the full multiplicative group: ord(g) = p−1 = 8190 = 2·3²·5·7·13.
*/
var FieldPrimitive uint32

func init() {
	FieldPrimitive = findPrimitiveRoot(FieldPrime)
}

/*
FermatPrime kept as alias during GF(257)→GF(8191) migration.
*/
const FermatPrime = FieldPrime

/*
InverseFermatBase kept as alias during migration.
*/
const InverseFermatBase = InverseBase

/*
MersenneReduce computes x mod (2^13 − 1) using the identity that for
Mersenne primes M = 2^k − 1, x mod M = (x & M) + (x >> k), iterated
until the result fits. Two iterations suffice for uint32 inputs because
the first pass yields at most 2^19 + 2^13 − 2 and the second yields
at most 2^7 + 2^13 − 2 < 2·M.
*/
func MersenneReduce(x uint32) uint32 {
	x = (x & mersenneMask) + (x >> mersenneShift)
	x = (x & mersenneMask) + (x >> mersenneShift)

	if x >= FieldPrime {
		x -= FieldPrime
	}

	return x
}

/*
MersenneReduce64 handles uint64 products that arise from Phase multiplication.
Folds 64 bits into 13-bit chunks via repeated Mersenne reduction.
*/
func MersenneReduce64(x uint64) uint32 {
	lo := uint32(x & uint64(mersenneMask))
	x >>= mersenneShift

	for x > 0 {
		lo += uint32(x & uint64(mersenneMask))
		x >>= mersenneShift
	}

	return MersenneReduce(lo)
}

/*
findPrimitiveRoot finds the smallest primitive root of GF(p) by checking
that g^((p-1)/q) ≠ 1 (mod p) for every prime factor q of p−1.
Panics if no root is found below p, which cannot happen for a prime field.
*/
func findPrimitiveRoot(p uint32) uint32 {
	pm1 := p - 1
	factors := primeFactors(pm1)

	for g := uint32(2); g < p; g++ {
		primitive := true

		for _, q := range factors {
			if modPow(g, pm1/q, p) == 1 {
				primitive = false
				break
			}
		}

		if primitive {
			return g
		}
	}

	panic("findPrimitiveRoot: no primitive root found (impossible for prime field)")
}

/*
primeFactors returns the distinct prime factors of n.
Used once at init to factor p−1 for primitive root verification.
*/
func primeFactors(n uint32) []uint32 {
	var factors []uint32

	for d := uint32(2); d*d <= n; d++ {
		if n%d == 0 {
			factors = append(factors, d)

			for n%d == 0 {
				n /= d
			}
		}
	}

	if n > 1 {
		factors = append(factors, n)
	}

	return factors
}

/*
modPow computes base^exp mod m via binary exponentiation.
Used at init for primitive root verification and at runtime for field power.
*/
func modPow(base, exp, m uint32) uint32 {
	result := uint32(1)
	base %= m

	for exp > 0 {
		if exp&1 == 1 {
			result = uint32(uint64(result) * uint64(base) % uint64(m))
		}

		base = uint32(uint64(base) * uint64(base) % uint64(m))
		exp >>= 1
	}

	return result
}

/*
Calculus provides algebraic operations for GF(8191) holographic inference.
All modular reductions use Mersenne-specific bit arithmetic for single-cycle
throughput on the critical path.
*/
type Calculus struct{}

type calcOpts func(*Calculus)

/*
NewCalculus instantiates a new Calculus for GF(8191) algebra.
*/
func NewCalculus(opts ...calcOpts) *Calculus {
	calc := &Calculus{}

	for _, opt := range opts {
		opt(calc)
	}

	return calc
}

/*
Sum strings into a Phase.
*/
func (calc *Calculus) Sum(s string) Phase {
	return calc.SumBytes([]byte(s))
}

/*
SumBytes hashes bytes into a Phase using the Phase Dial primes as
orthogonal positional multipliers. The Mersenne reduction replaces
generic modulo to keep this on the fast path.
*/
func (calc *Calculus) SumBytes(b []byte) Phase {
	var sum uint64
	numPrimes := len(Primes)

	for i := range b {
		primeMult := uint32(1)

		if numPrimes > 0 {
			primeMult = uint32(Primes[i%numPrimes])
		}

		sum += uint64(b[i]) * uint64(primeMult)
	}

	res := MersenneReduce64(sum)

	if res == 0 && len(b) > 0 {
		return 1
	}

	return Phase(res)
}

/*
Multiply computes the GF(8191) product of two phases using Mersenne reduction
on the 64-bit intermediate product.
*/
func (calc *Calculus) Multiply(a, b Phase) Phase {
	return Phase(MersenneReduce64(uint64(a) * uint64(b)))
}

/*
Add computes the GF(8191) sum of two phases.
*/
func (calc *Calculus) Add(a, b Phase) Phase {
	return Phase(MersenneReduce(uint32(a) + uint32(b)))
}

/*
Subtract computes the GF(8191) difference of two phases.
*/
func (calc *Calculus) Subtract(a, b Phase) Phase {
	return Phase(MersenneReduce(uint32(a) + FieldPrime - uint32(b)))
}

/*
Power calculates modular exponentiation in GF(8191) via binary squaring
with Mersenne reduction at each step to avoid overflow in the 64-bit
intermediate products.
*/
func (calc *Calculus) Power(base Phase, exp uint32) Phase {
	return Phase(modPow(uint32(base), exp, FieldPrime))
}

/*
Inverse calculates the Fermat's Little Theorem modular inverse of a GF(8191) Phase.
Returns ErrZeroInverse when a is zero (zero has no multiplicative inverse).
*/
func (calc *Calculus) Inverse(a Phase) (Phase, error) {
	if a == Phase(0) {
		return 0, ErrZeroInverse
	}

	return calc.Power(a, InverseBase), nil
}

/*
CalculusError is a typed error for Calculus failures.
*/
type CalculusError string

const (
	ErrZeroInverse CalculusError = "cannot invert zero phase"
)

/*
Error implements the error interface for CalculusError.
*/
func (err CalculusError) Error() string {
	return string(err)
}
