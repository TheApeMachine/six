package primitive

/*
Primes holds the first CoreBits prime numbers, indexed by bit position.
Position 0 → 2, position 1 → 3, position 2 → 5, position 3 → 7, etc.
Generated once at init via a sieve of Eratosthenes up to an upper bound
derived from Rosser's theorem: p_n < n(ln n + ln ln n).
*/
var Primes [CoreBits]uint32

/*
PrimeIndex maps a prime number back to its bit position in the Value
field. Only populated for the first CoreBits primes.
*/
var PrimeIndex map[uint32]int

/*
baseValues precomputes the square-free prime projection for every byte.
This turns base projection into a single fixed-size Value copy.
*/
var baseValues [256]Value

func init() {
	const sieveLimit = 100_000

	sieve := [sieveLimit]bool{}

	for i := 2; i < sieveLimit; i++ {
		sieve[i] = true
	}

	for i := 2; i*i < sieveLimit; i++ {
		if !sieve[i] {
			continue
		}

		for j := i * i; j < sieveLimit; j += i {
			sieve[j] = false
		}
	}

	PrimeIndex = make(map[uint32]int, CoreBits)
	count := 0

	for n := 2; n < sieveLimit && count < CoreBits; n++ {
		if !sieve[n] {
			continue
		}

		Primes[count] = uint32(n)
		PrimeIndex[uint32(n)] = count
		count++
	}

	for byteValue := range 256 {
		n := uint32(byteValue)

		if n < 2 {
			continue
		}

		for i := 0; i < CoreBits && Primes[i]*Primes[i] <= n; i++ {
			if n%Primes[i] != 0 {
				continue
			}

			baseValues[byteValue].Set(i)

			for n%Primes[i] == 0 {
				n /= Primes[i]
			}
		}

		if n > 1 {
			if pos, ok := PrimeIndex[n]; ok {
				baseValues[byteValue].Set(pos)
			}
		}
	}
}

/*
BaseValue projects a single byte into the prime-indexed field by
activating the bit positions that correspond to the byte's prime
factors. Bytes 0 and 1 have no prime factors and produce zero Values.
*/
func BaseValue(b byte) *Value {
	v := baseValues[b]

	return &v
}

/*
BaseValueInto projects a byte into dst without allocating.
The destination is fully overwritten on every call.
*/
func BaseValueInto(dst *Value, b byte) {
	*dst = baseValues[b]
}
