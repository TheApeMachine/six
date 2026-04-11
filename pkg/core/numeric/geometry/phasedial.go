package geometry

import (
	"math"
	"math/cmplx"

	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
PhaseDialDimensions matches numeric.PhaseDialPrimeCount: one complex
component per prime frequency ω_k.
*/
const PhaseDialDimensions = numeric.PhaseDialPrimeCount

/*
PhaseDial is a high-dimensional complex vector of rotational phase gradients.
Each dimension uses a prime frequency ω to accumulate phase from sequence
position and a structural mix of the Value words; encoding normalizes to
unit L2 magnitude for cosine similarity and composition.
*/
type PhaseDial []complex128

/*
NewPhaseDial allocates a zero-initialized PhaseDial.
*/
func NewPhaseDial() PhaseDial {
	return make(PhaseDial, PhaseDialDimensions)
}

/*
structuralPhaseMix folds the first eight uint64 words of a Value into a
scalar in [0,1) so repetitive token packing still leaves discriminative
phase when the Morton slab saturates.
*/
func structuralPhaseMix(value *primitive.Value) float64 {
	if value == nil {
		return 0
	}

	var mix uint64

	const wordBlocks = 8

	for blk := 0; blk < wordBlocks && blk < len(*value); blk++ {
		mix ^= (*value)[blk] * (0x9e3779b185ebca87 + uint64(blk+1)*0x6c62272e07bb0142)
	}

	return float64(mix>>32) * (1.0 / float64(1<<32))
}

/*
EncodeFromValues generates a PhaseDial from a value sequence.
Value-native: uses word structure and position for phase; no raw byte scan.
*/
func (dial PhaseDial) EncodeFromValues(values []primitive.Value) PhaseDial {
	if len(values) == 0 {
		return dial
	}

	if len(dial) < PhaseDialDimensions {
		dial = NewPhaseDial()
	}

	for k := 0; k < PhaseDialDimensions; k++ {
		var sum complex128

		omega := float64(numeric.PhaseDialPrimes[k])

		for t := range values {
			structuralPhase := structuralPhaseMix(&values[t])
			phase := (omega * float64(t+1) * 0.1) + (structuralPhase * math.Pi * 2)
			sum += cmplx.Rect(1.0, phase)
		}

		dial[k] = sum
	}

	return dial.normalize()
}

/*
AddValuePhase incrementally adds a single value's phase to an unnormalized PhaseDial.
*/
func (dial PhaseDial) AddValuePhase(value primitive.Value, position int) {
	if len(dial) < PhaseDialDimensions {
		return
	}

	structuralPhase := structuralPhaseMix(&value)

	for k := 0; k < PhaseDialDimensions; k++ {
		omega := float64(numeric.PhaseDialPrimes[k])
		phase := (omega * float64(position+1) * 0.1) + (structuralPhase * math.Pi * 2)
		dial[k] += cmplx.Rect(1.0, phase)
	}
}

/*
CopyAndNormalize returns a cloned, unit-normalized copy of the dial.
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

	factor := cmplx.Rect(1.0, angleRadians)
	out := make(PhaseDial, len(dial))

	for k, val := range dial {
		out[k] = val * factor
	}

	return out
}

/*
Similarity returns cosine similarity between two PhaseDial vectors
(real part of normalized Hermitian inner product).
*/
func (dial PhaseDial) Similarity(other PhaseDial) float64 {
	if len(dial) != len(other) || len(dial) == 0 {
		return 0
	}

	var dot complex128

	var normA float64

	var normB float64

	for i := range dial {
		dot += cmplx.Conj(dial[i]) * other[i]
		reA, imA := real(dial[i]), imag(dial[i])
		reB, imB := real(other[i]), imag(other[i])
		normA += reA*reA + imA*imA
		normB += reB*reB + imB*imB
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return real(dot) / (math.Sqrt(normA) * math.Sqrt(normB))
}

/*
ComposeMidpoint returns Normalize(Normalize(a) + Normalize(b)).
*/
func (dial PhaseDial) ComposeMidpoint(other PhaseDial) PhaseDial {
	if len(dial) != len(other) || len(dial) == 0 {
		return nil
	}

	nA := dial.norm()
	nB := other.norm()
	out := make(PhaseDial, len(dial))

	var energy float64

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
		re, im := real(out[k]), imag(out[k])
		energy += re*re + im*im
	}

	if energy > 0 {
		scale := math.Sqrt(energy)

		for k := range out {
			out[k] = complex(real(out[k])/scale, imag(out[k])/scale)
		}
	}

	return out
}

func (dial PhaseDial) norm() float64 {
	var total float64

	for _, v := range dial {
		re, im := real(v), imag(v)
		total += re*re + im*im
	}

	return math.Sqrt(total)
}

func (dial PhaseDial) normalize() PhaseDial {
	var sumSq float64

	for _, val := range dial {
		re, im := real(val), imag(val)
		sumSq += re*re + im*im
	}

	if sumSq == 0 {
		return dial
	}

	inv := 1.0 / math.Sqrt(sumSq)

	for i := range dial {
		dial[i] = complex(real(dial[i])*inv, imag(dial[i])*inv)
	}

	return dial
}

/*
PhaseRotor is a PhaseDialDimensions-length array of PGA multivectors. Each
dimension uses a Fibonacci-lattice axis on S² with the same ω_k phase law
as PhaseDial, lifting planar phase into Cl(3,0,1) even subalgebra.
*/
type PhaseRotor []Multivector

/*
NewPhaseRotor allocates a zero-initialized PhaseRotor.
*/
func NewPhaseRotor() PhaseRotor {
	return make(PhaseRotor, PhaseDialDimensions)
}

/*
EncodeFromValues generates a PhaseRotor from a value sequence.
*/
func (rotor PhaseRotor) EncodeFromValues(values []primitive.Value) PhaseRotor {
	if len(values) == 0 {
		return rotor
	}

	if len(rotor) < PhaseDialDimensions {
		rotor = NewPhaseRotor()
	}

	goldenAngle := math.Pi * (3 - math.Sqrt(5))
	nBasis := float64(PhaseDialDimensions)

	for k := 0; k < PhaseDialDimensions; k++ {
		theta := goldenAngle * float64(k)
		zCoord := 1 - (2*float64(k)+1)/nBasis
		radius := math.Sqrt(math.Max(0, 1-zCoord*zCoord))

		axisE23 := radius * math.Cos(theta)
		axisE31 := radius * math.Sin(theta)
		axisE12 := zCoord

		omega := float64(numeric.PhaseDialPrimes[k])

		var sum Multivector

		for t := range values {
			structuralPhase := structuralPhaseMix(&values[t])
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
Similarity averages the scalar part of rotor[k]·other[k]† across dimensions.
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
ToDialCompat projects each rotor to a unit complex number per dimension
for consumers that expect a PhaseDial.
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
