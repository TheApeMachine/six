package numeric

import "math"

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

type Numerics struct {
	BasisPrimes []int32
}

func NewNumerics() *Numerics {
	numerics := &Numerics{
		BasisPrimes: make([]int32, 512),
	}

	numerics.SieveOfEratosthenes(4000) // Upper bound for 512 primes is 3671
	return numerics
}

func (numerics *Numerics) SumSinCos(phases []float64) (float64, float64) {
	sinSum := 0.0
	cosSum := 0.0

	for _, phi := range phases {
		sinSum += math.Sin(phi)
		cosSum += math.Cos(phi)
	}

	return sinSum, cosSum
}

func (numerics *Numerics) CircularDistance(a, b float64) float64 {
	d := math.Mod(a-b+math.Pi, 2*math.Pi)

	if d < 0 {
		d += 2 * math.Pi
	}

	return d - math.Pi
}

func (numerics *Numerics) SieveOfEratosthenes(n int) {
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
	for i := 2; i < n && idx < 512; i++ {
		if !checked[i] {
			numerics.BasisPrimes[idx] = int32(i)
			idx++
		}
	}
}
