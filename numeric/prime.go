package numeric

import "math"

/*
NSymbols is the number of symbols in the alphabet.
We use raw bytes as symbols, so we need 256 symbols.
*/
const NSymbols = 256

/*
NBasis is the number of basis prime oscillators. 512 primes gives us
an exact mapping to a 512-bit bitset.
*/
const NBasis = 512

/*
ChordBlocks is the number of uint64 blocks in a Chord bitset. Derived from
NBasis so changing NBasis (e.g. to 1024) automatically yields ChordBlocks=16.
*/
const ChordBlocks = NBasis / 64

/*
FrequencySpread is the number of octaves to spread the frequency across.
*/
var FrequencySpread = math.Log2(float64(NBasis))

/*
FibWindows is the Fibonacci sequence of window sizes used for multi-scale
co-occurrence and eigen initialization. Small windows (3–8) capture
fine-grained local correlation; larger windows (13–21) capture longer-range
coupling. Works for any token stream — text, images, audio — no modality-specific
assumptions.

Bounds: 3 is the smallest window with non-trivial co-occurrence structure;
21 is an upper limit before the matrix becomes too sparse for reliable
eigenvectors.
*/
var FibWindows = []int{3, 5, 8, 13, 21}

/*
FibWeights are the mixing weights for each Fibonacci window, summing to 1.0.
Derived from FibWindows as 1/window (inverse scale): local correlation is
denser per byte than long-range; smaller windows get higher weight.
*/
var FibWeights []float64

func init() {
	var sum float64

	for _, w := range FibWindows {
		sum += 1.0 / float64(w)
	}

	FibWeights = make([]float64, len(FibWindows))

	for i, w := range FibWindows {
		FibWeights[i] = (1.0 / float64(w)) / sum
	}
}

type Prime struct {
	Basis []int32
}

var prime *Prime

func init() {
	prime = New()
}

func New() *Prime {
	prime := &Prime{
		Basis: make([]int32, NBasis),
	}

	prime.SieveOfEratosthenes(4000) // Upper bound for 512 primes is 3671
	return prime
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

	for i := 2; i < n && idx < NBasis; i++ {
		if !checked[i] {
			prime.Basis[idx] = int32(i)
			idx++
		}
	}
}
