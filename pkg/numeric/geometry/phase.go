package geometry

import (
	"context"
	"fmt"
	"math"
	"math/cmplx"

	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/system/console"
	config "github.com/theapemachine/six/pkg/system/core"
	"github.com/theapemachine/six/pkg/system/pool"
)

/*
PhaseDial is a 512-dimensional complex vector representing rotational phase gradients.
Each dimension uses a prime frequency (omega) to accumulate phase from position and
structural identity; encoding produces unit-normalized vectors suitable for cosine
similarity and vector addition. Use EncodeFromValues for value sequences.
*/
type PhaseDial []complex128

/*
NewPhaseDial allocates a zero-initialized PhaseDial of NBasis dimensions.
Used before EncodeFromValues to produce a unit-normalized phase vector.
*/
func NewPhaseDial() PhaseDial {
	return make(PhaseDial, config.Numeric.NBasis)
}

/*
EncodeFromValues generates a 512-dim PhaseDial from a value sequence.
Value-native: uses value structure (active primes) and position for phase;
no raw bytes. The value's prime signature maps to rotational phase gradients.
*/
func (dial PhaseDial) EncodeFromValues(values []primitive.Value) PhaseDial {
	if len(values) == 0 {
		return dial
	}

	for k := 0; k < config.Numeric.NBasis; k++ {
		var sum complex128
		omega := float64(numeric.Primes[k])

		for t := range values {
			// Fold all 8 raw value words into a structural float.
			// This preserves discrimination even when ValueBin collapses
			// (e.g. when highly repetitive data saturates the 257-bit space).
			var mix uint64
			for blk := range config.ValueBlocks {
				mix ^= values[t].Block(blk) * (0x9e3779b185ebca87 + uint64(blk+1)*0x6c62272e07bb0142)
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
EncodeFromValuesParallel is the pool-accelerated variant of EncodeFromValues.
The outer loop over NBasis dimensions is embarrassingly parallel — each k writes
to dial[k] with no cross-dependency — so we fan out to the pool and join before
the serial normalize pass.

If p is nil the call falls back to the serial EncodeFromValues path.
*/
func (dial PhaseDial) EncodeFromValuesParallel(values []primitive.Value, p interface {
	Schedule(string, func(context.Context) (any, error), ...pool.JobOption) chan *pool.Result
}) PhaseDial {
	if p == nil {
		return dial.EncodeFromValues(values)
	}
	if len(values) == 0 {
		return dial
	}

	// Pre-compute the structural phase per value once; it's the same for all k.
	structuralPhases := make([]float64, len(values))
	for t := range values {
		var mix uint64
		for blk := range config.ValueBlocks {
			mix ^= values[t].Block(blk) * (0x9e3779b185ebca87 + uint64(blk+1)*0x6c62272e07bb0142)
		}
		structuralPhases[t] = float64(mix>>32) * (1.0 / float64(1<<32))
	}

	nBasis := config.Numeric.NBasis

	/*
		scheduledDimension pairs a dimension index with a result channel for
		asynchronous pool tasks. ch receives *pool.Result values; dimension is the
		associated dimension index for logging and scheduling.
	*/
	type scheduledDimension struct {
		ch        chan *pool.Result
		dimension int
	}

	waiting := make([]scheduledDimension, 0, nBasis)

	for k := range nBasis {
		kk := k
		omega := float64(numeric.Primes[kk])

		resCh := p.Schedule(fmt.Sprintf("phasedial-k%d", kk), func(ctx context.Context) (any, error) {
			var sum complex128
			for t := range values {
				phase := (omega * float64(t+1) * 0.1) + (structuralPhases[t] * math.Pi * 2)
				sum += cmplx.Rect(1.0, phase)
			}
			dial[kk] = sum // each k owns a distinct index — no race
			return nil, nil
		})

		if resCh == nil {
			continue
		}

		waiting = append(waiting, scheduledDimension{
			ch:        resCh,
			dimension: kk,
		})
	}

	for _, scheduled := range waiting {
		result := <-scheduled.ch
		if result != nil && result.Error != nil {
			_ = console.Error(result.Error, "dimension", scheduled.dimension)
		}
	}

	return dial.normalize()
}

/*
AddValuePhase incrementally adds a single value's phase to an unnormalized PhaseDial.
This allows O(N) instead of O(N^2) sequential dial construction.
*/
func (dial PhaseDial) AddValuePhase(value primitive.Value, t int) {
	var mix uint64
	for blk := range config.ValueBlocks {
		mix ^= value.Block(blk) * (0x9e3779b185ebca87 + uint64(blk+1)*0x6c62272e07bb0142)
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
EncodeFromValues calls this before returning; preserves receiver in-place.
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

/*
PhaseRotor is a 512-dimensional Clifford rotor array. Each dimension uses
a PGA Multivector instead of a complex number, encoding rotation in a
unique plane derived from the Fibonacci lattice. This lifts PhaseDial's
2D phase rotations into the full even subalgebra of Cl(3,0,1), enabling
spatial reasoning beyond the complex plane.
*/
type PhaseRotor []Multivector

/*
NewPhaseRotor allocates a zero-initialized PhaseRotor of NBasis dimensions.
Each element is a zero Multivector; call EncodeFromValues to populate.
*/
func NewPhaseRotor() PhaseRotor {
	return make(PhaseRotor, config.Numeric.NBasis)
}

/*
EncodeFromValues generates a 512-dim PhaseRotor from a value sequence.
Each dimension k gets a unique rotation axis via the Fibonacci lattice
on S², using prime frequency omega_k to accumulate phase from position
and structural identity — identical to PhaseDial's phase formula. The
per-value rotors are summed (quaternion-mean style) then normalized to
unit versors.
*/
func (rotor PhaseRotor) EncodeFromValues(values []primitive.Value) PhaseRotor {
	if len(values) == 0 {
		return rotor
	}

	goldenAngle := math.Pi * (3 - math.Sqrt(5))
	nBasis := float64(config.Numeric.NBasis)

	for k := 0; k < config.Numeric.NBasis; k++ {
		theta := goldenAngle * float64(k)
		zCoord := 1 - (2*float64(k)+1)/nBasis
		rCoord := math.Sqrt(1 - zCoord*zCoord)

		axisE23 := rCoord * math.Cos(theta)
		axisE31 := rCoord * math.Sin(theta)
		axisE12 := zCoord

		omega := float64(numeric.Primes[k])

		var sum Multivector

		for t := range values {
			var mix uint64

			for blk := range config.ValueBlocks {
				mix ^= values[t].Block(blk) * (0x9e3779b185ebca87 + uint64(blk+1)*0x6c62272e07bb0142)
			}

			structuralPhase := float64(mix>>32) * (1.0 / float64(1<<32))
			phase := (omega * float64(t+1) * 0.1) + (structuralPhase * math.Pi * 2)
			halfPhase := phase / 2
			sinHalf := math.Sin(halfPhase)
			cosHalf := math.Cos(halfPhase)

			sum[MvScalar] += cosHalf
			sum[MvE12] += sinHalf * axisE12
			sum[MvE31] += sinHalf * axisE31
			sum[MvE23] += sinHalf * axisE23
		}

		rotor[k] = sum.Normalize()
	}

	return rotor
}

/*
Similarity returns the rotor-space similarity between two PhaseRotors.
For each dimension k, the scalar part of rotor[k]·other[k]† measures the
cosine of the half-angle between the two rotors (analogous to quaternion
dot product). The returned value averages this across all dimensions,
giving 1.0 for identical rotors and values near 0 for uncorrelated ones.
*/
func (rotor PhaseRotor) Similarity(other PhaseRotor) float64 {
	if len(rotor) != len(other) || len(rotor) == 0 {
		return 0
	}

	var dotSum float64

	for k := range rotor {
		product := rotor[k].GeometricProduct(other[k].Reverse())
		dotSum += product[MvScalar]
	}

	return dotSum / float64(len(rotor))
}

/*
ToDialCompat projects each rotor down to a 2D phase angle for backward
compatibility with PhaseDial consumers. The total rotation angle
θ = 2·atan2(|bivector|, scalar) is extracted and signed by the net
bivector orientation, then mapped to a unit complex number.
*/
func (rotor PhaseRotor) ToDialCompat() PhaseDial {
	dial := make(PhaseDial, len(rotor))

	for k, mv := range rotor {
		eucNorm := math.Sqrt(
			mv[MvE12]*mv[MvE12] +
				mv[MvE31]*mv[MvE31] +
				mv[MvE23]*mv[MvE23],
		)

		angle := 2 * math.Atan2(eucNorm, mv[MvScalar])

		if mv[MvE12]+mv[MvE31]+mv[MvE23] < 0 {
			angle = -angle
		}

		dial[k] = cmplx.Rect(1.0, angle)
	}

	return dial.normalize()
}
