package geometry

import (
	"context"
	"fmt"
	"math"
	"math/cmplx"
	"sync"

	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
	config "github.com/theapemachine/six/pkg/system/core"
	"github.com/theapemachine/six/pkg/system/console"
	"github.com/theapemachine/six/pkg/system/pool"
)

/*
PhaseDial is a 512-dimensional complex vector representing rotational phase gradients.
Each dimension uses a prime frequency (omega) to accumulate phase from position and
structural identity; encoding produces unit-normalized vectors suitable for cosine
similarity and vector addition. Use EncodeFromChords for chord sequences.
*/
type PhaseDial []complex128

/*
NewPhaseDial allocates a zero-initialized PhaseDial of NBasis dimensions.
Used before EncodeFromChords to produce a unit-normalized phase vector.
*/
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
			// Fold all 8 raw chord words into a structural float.
			// This preserves discrimination even when ChordBin collapses
			// (e.g. when highly repetitive data saturates the 257-bit space).
			var mix uint64
			for blk := range config.ChordBlocks {
				mix ^= chords[t].Block(blk) * (0x9e3779b185ebca87 + uint64(blk+1)*0x6c62272e07bb0142)
			}
			structuralPhase := float64(mix>>32) * (1.0 / float64(1<<32))

			phase := (omega * float64(t+1) * 0.1) + (structuralPhase * math.Pi * 2)
			sum += cmplx.Rect(1.0, phase)
		}

		dial[k] = sum
	}

	return dial.normalize()
}

/*
EncodeFromChordsParallel is the pool-accelerated variant of EncodeFromChords.
The outer loop over NBasis dimensions is embarrassingly parallel — each k writes
to dial[k] with no cross-dependency — so we fan out to the pool and join before
the serial normalize pass.

If p is nil the call falls back to the serial EncodeFromChords path.
*/
func (dial PhaseDial) EncodeFromChordsParallel(chords []data.Chord, p interface {
	Schedule(string, func(context.Context) (any, error), ...pool.JobOption) chan *pool.Result
}) PhaseDial {
	if p == nil {
		return dial.EncodeFromChords(chords)
	}
	if len(chords) == 0 {
		return dial
	}

	// Pre-compute the structural phase per chord once; it's the same for all k.
	structuralPhases := make([]float64, len(chords))
	for t := range chords {
		var mix uint64
		for blk := range config.ChordBlocks {
			mix ^= chords[t].Block(blk) * (0x9e3779b185ebca87 + uint64(blk+1)*0x6c62272e07bb0142)
		}
		structuralPhases[t] = float64(mix>>32) * (1.0 / float64(1<<32))
	}

	nBasis := config.Numeric.NBasis

	var wg sync.WaitGroup

	for k := range nBasis {
		kk := k
		omega := float64(numeric.Primes[kk])

		resCh := p.Schedule(fmt.Sprintf("phasedial-k%d", kk), func(ctx context.Context) (any, error) {
			var sum complex128
			for t := range chords {
				phase := (omega * float64(t+1) * 0.1) + (structuralPhases[t] * math.Pi * 2)
				sum += cmplx.Rect(1.0, phase)
			}
			dial[kk] = sum // each k owns a distinct index — no race
			return nil, nil
		})

		if resCh == nil {
			continue
		}

		wg.Add(1)
		go func(ch chan *pool.Result, dimension int) {
			defer wg.Done()
			res := <-ch
			if res != nil && res.Error != nil {
				_ = console.Error(res.Error, "dimension", dimension)
			}
		}(resCh, kk)
	}

	wg.Wait()
	return dial.normalize()
}

/*
AddChordPhase incrementally adds a single chord's phase to an unnormalized PhaseDial.
This allows O(N) instead of O(N^2) sequential dial construction.
*/
func (dial PhaseDial) AddChordPhase(chord data.Chord, t int) {
	var mix uint64
	for blk := range config.ChordBlocks {
		mix ^= chord.Block(blk) * (0x9e3779b185ebca87 + uint64(blk+1)*0x6c62272e07bb0142)
	}
	structuralPhase := float64(mix>>32) * (1.0 / float64(1<<32))

	for k := 0; k < config.Numeric.NBasis; k++ {
		omega := float64(numeric.Primes[k])
		phase := (omega * float64(t+1) * 0.1) + (structuralPhase * math.Pi * 2)
		dial[k] += cmplx.Rect(1.0, phase)
	}
}

/*
CopyAndNormalize returns a cloned, normalized copy of the dial.
Useful when accumulating phases incrementally.
*/
func (dial PhaseDial) CopyAndNormalize() PhaseDial {
	out := make(PhaseDial, len(dial))
	copy(out, dial)
	return out.normalize()
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
Similarity returns cosine similarity between two PhaseDial vectors
(real part of normalized inner product).
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

/*
norm returns the L2 magnitude of the dial (sqrt of sum of squared components).
Used internally by ComposeMidpoint and Similarity; exported methods use normalize.
*/
func (dial PhaseDial) norm() float64 {
	var n float64
	for _, v := range dial {
		n += real(v)*real(v) + imag(v)*imag(v)
	}
	return math.Sqrt(n)
}

/*
normalize divides each component by the L2 norm so the dial has unit magnitude.
EncodeFromChords calls this before returning; preserves receiver in-place.
*/
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
