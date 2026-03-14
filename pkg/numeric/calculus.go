package numeric

/*
Phase is a value in the GF(257) field, representing a state, rotation, or semantic anchor.
*/
type Phase uint32

const (
	FermatPrime       uint32 = 257
	FermatPrimitive   uint32 = 3
	InverseFermatBase uint32 = 255
)

/*
Calculus provides algebraic operations for GF(257) holographic inference.
It allows calculation of modular exponents, inverses, and string phases.
*/
type Calculus struct{}

type calcOpts func(*Calculus)

/*
NewCalculus instantiates a new Calculus for GF(257) algebra.
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
SumBytes hashes bytes into a Phase.
*/
func (calc *Calculus) SumBytes(b []byte) Phase {
	var sum uint32
	for i := range b {
		sum += uint32(b[i])
	}
	return Phase(sum % FermatPrime)
}

/*
Multiply computes the GF(257) product of two phases.
*/
func (calc *Calculus) Multiply(a, b Phase) Phase {
	return Phase((uint32(a) * uint32(b)) % FermatPrime)
}

/*
Add computes the GF(257) sum of two phases.
*/
func (calc *Calculus) Add(a, b Phase) Phase {
	return Phase((uint32(a) + uint32(b)) % FermatPrime)
}

/*
Subtract computes the GF(257) difference of two phases.
*/
func (calc *Calculus) Subtract(a, b Phase) Phase {
	return Phase((uint32(a) + FermatPrime - uint32(b)) % FermatPrime)
}

/*
Power calculates the Fermat prime exponential via fast modular exponentiation.
*/
func (calc *Calculus) Power(base Phase, exp uint32) Phase {
	res := uint32(1)
	b := uint32(base) % FermatPrime

	for exp > 0 {
		if exp&1 == 1 {
			res = (res * b) % FermatPrime
		}
		b = (b * b) % FermatPrime
		exp >>= 1
	}

	return Phase(res)
}

/*
Inverse calculates the Fermat's Little Theorem modular inverse of a GF(257) Phase.
Returns ErrZeroInverse when a is zero (zero has no multiplicative inverse).
*/
func (calc *Calculus) Inverse(a Phase) (Phase, error) {
	if a == Phase(0) {
		return 0, ErrZeroInverse
	}
	return calc.Power(a, InverseFermatBase), nil
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
