package geometry

import (
	"math"
	"math/cmplx"

	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/numeric"
)

type PhaseDial []complex128

func NewPhaseDial() PhaseDial {
	return make(PhaseDial, config.Numeric.NBasis)
}

/*
EncodeFromChords generates a 512-dim PhaseDial from a chord sequence.
Chord-native: uses chord structure (active primes) and position for phase;
no raw bytes. The chord's prime signature maps to rotational phase gradients.
*/
func (dial PhaseDial) EncodeFromChords(chords []data.Chord) PhaseDial {
	if len(chords) == 0 {
		return dial
	}

	for k := 0; k < config.Numeric.NBasis; k++ {
		var sum complex128
		omega := float64(numeric.Primes[k])

		for t := range chords {
			// Structural phase from chord: sum of primes at active bits in dimension k's neighborhood
			// Use ChordBin as a structural identity proxy for phase contribution
			bin := data.ChordBin(&chords[t])
			structuralPrime := float64(numeric.Primes[bin%config.Numeric.NSymbols])

			phase := (omega * float64(t+1) * 0.1) + (structuralPrime * 0.1)
			sum += cmplx.Rect(1.0, phase)
		}

		dial[k] = sum
	}

	return dial.normalize()
}

/*
Encode takes a raw byte sequence and generates a 512-dimension PhaseDial.
Legacy entrypoint for text; prefer EncodeFromChords for chord-native pipelines.
*/
func (dial PhaseDial) Encode(text string) PhaseDial {
	bytes := []byte(text)

	if len(bytes) == 0 {
		return dial
	}

	for k := 0; k < config.Numeric.NBasis; k++ {
		var sum complex128
		omega := float64(numeric.Primes[k])

		for t, b := range bytes {
			symbolPrime := float64(numeric.Primes[int(b)%config.Numeric.NSymbols])
			phase := (omega * float64(t+1) * 0.1) + (symbolPrime * 0.1)
			sum += cmplx.Rect(1.0, phase)
		}

		dial[k] = sum
	}

	return dial.normalize()
}

/*
Rotate applies a global phase rotation e^{iθ} to each dimension.
Returns a new PhaseDial; the receiver is unchanged.
*/
func (dial PhaseDial) Rotate(angleRadians float64) PhaseDial {
	if len(dial) == 0 {
		return nil
	}
	f := cmplx.Rect(1.0, angleRadians)
	out := make(PhaseDial, len(dial))
	for k, val := range dial {
		out[k] = val * f
	}
	return out
}

/*
Similarity returns cosine similarity between two PhaseDial vectors (real part of normalized inner product).
*/
func (dial PhaseDial) Similarity(other PhaseDial) float64 {
	if len(dial) != len(other) || len(dial) == 0 {
		return 0
	}
	var dot complex128
	var normA, normB float64
	for i := range dial {
		dot += cmplx.Conj(dial[i]) * other[i]
		normA += real(dial[i])*real(dial[i]) + imag(dial[i])*imag(dial[i])
		normB += real(other[i])*real(other[i]) + imag(other[i])*imag(other[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return real(dot) / (math.Sqrt(normA) * math.Sqrt(normB))
}

/*
ComposeMidpoint returns the geometric midpoint of two PhaseDial vectors:
Normalize(Normalize(a) + Normalize(b)). Used for two-hop composition.
*/
func (dial PhaseDial) ComposeMidpoint(other PhaseDial) PhaseDial {
	if len(dial) != len(other) || len(dial) == 0 {
		return nil
	}
	nA := dial.norm()
	nB := other.norm()
	out := make(PhaseDial, len(dial))
	var n float64
	for k := range dial {
		vA := dial[k]
		if nA > 0 {
			vA /= complex(nA, 0)
		}
		vB := other[k]
		if nB > 0 {
			vB /= complex(nB, 0)
		}
		out[k] = vA + vB
		r, im := real(out[k]), imag(out[k])
		n += r*r + im*im
	}
	if n > 0 {
		n = math.Sqrt(n)
		for k := range out {
			out[k] = complex(real(out[k])/n, imag(out[k])/n)
		}
	}
	return out
}

func (dial PhaseDial) norm() float64 {
	var n float64
	for _, v := range dial {
		n += real(v)*real(v) + imag(v)*imag(v)
	}
	return math.Sqrt(n)
}

func (dial PhaseDial) normalize() PhaseDial {
	var norm float64

	for _, val := range dial {
		r, i := real(val), imag(val)
		norm += r*r + i*i
	}

	if norm == 0 {
		return dial
	}

	norm = math.Sqrt(norm)

	for i := range dial {
		dial[i] = complex(real(dial[i])/norm, imag(dial[i])/norm)
	}

	return dial
}
