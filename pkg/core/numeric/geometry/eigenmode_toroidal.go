package geometry

import (
	"math"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
DefaultToroidalClosureRadians is the default maximum angular distance on the
theta torus for IsGeometricallyClosed when no explicit tolerance is passed.
*/
const DefaultToroidalClosureRadians = math.Pi / 32

/*
EigenMode maps values to toroidal phase (Theta, Phi) from their 257-bit
affinity geometry. Analytical: no transition matrices; phase derives from
set-bit indices as angles 2π·idx/257 and from total affinity popcount.

Theta: circular mean of active lane angles. Phi: 2π·AffinityPopcount/257.
*/
type EigenMode struct {
	Trained bool
}

/*
eigenModeOpts configures EigenMode at construction.
*/
type eigenModeOpts func(*EigenMode)

/*
NewEigenMode creates a stateless, value-native phase evaluator.
*/
func NewEigenMode(opts ...eigenModeOpts) *EigenMode {
	eigen := &EigenMode{
		Trained: true,
	}

	for _, opt := range opts {
		opt(eigen)
	}

	return eigen
}

/*
BuildMultiScaleCooccurrence is a no-op; the analytical EigenMode needs no
transition matrix or eigendecomposition.
*/
func (eigen *EigenMode) BuildMultiScaleCooccurrence(values []primitive.Value) error {
	eigen.Trained = true

	return nil
}

/*
PhaseForValue maps a single value to (theta, phi) from its affinity bits.
*/
func (eigen *EigenMode) PhaseForValue(value *primitive.Value) (theta, phi float64) {
	if value == nil {
		return 0, 0
	}

	indices := value.AffinityBitIndices()

	var sinSum float64

	var cosSum float64

	for _, idx := range indices {
		angle := 2 * math.Pi * float64(idx) / 257.0
		sinSum += math.Sin(angle)
		cosSum += math.Cos(angle)
	}

	if sinSum == 0 && cosSum == 0 {
		theta = 0
	} else {
		theta = math.Atan2(sinSum, cosSum)
	}

	phi = 2 * math.Pi * float64(value.AffinityPopcount()) / 257.0

	return theta, phi
}

/*
SeqToroidalMeanPhase returns circular means of intrinsic phases for a sequence.
*/
func (eigen *EigenMode) SeqToroidalMeanPhase(values []primitive.Value) (theta, phi float64) {
	n := len(values)

	if n == 0 {
		return 0, 0
	}

	var sinTSum float64

	var cosTSum float64

	var sinPSum float64

	var cosPSum float64

	for i := range values {
		tTheta, tPhi := eigen.PhaseForValue(&values[i])
		sinTSum += math.Sin(tTheta)
		cosTSum += math.Cos(tTheta)
		sinPSum += math.Sin(tPhi)
		cosPSum += math.Cos(tPhi)
	}

	return math.Atan2(sinTSum, cosTSum), math.Atan2(sinPSum, cosPSum)
}

/*
WeightedCircularMean returns the circular mean of theta weighted by affinity
popcount, and a concentration in [0,1] as |R|/wSum.
*/
func (eigen *EigenMode) WeightedCircularMean(values []primitive.Value) (phase float64, concentration float64) {
	if len(values) == 0 {
		return 0, 0
	}

	var sinSum float64

	var cosSum float64

	var weightSum float64

	for i := range values {
		theta, _ := eigen.PhaseForValue(&values[i])
		weight := float64(values[i].AffinityPopcount())

		if weight <= 0 {
			weight = 1
		}

		sinSum += weight * math.Sin(theta)
		cosSum += weight * math.Cos(theta)
		weightSum += weight
	}

	if weightSum == 0 {
		return 0, 0
	}

	phase = math.Atan2(sinSum, cosSum)
	concentration = math.Sqrt(sinSum*sinSum+cosSum*cosSum) / weightSum

	return phase, concentration
}

/*
IsGeometricallyClosed reports whether the weighted mean theta is within
DefaultToroidalClosureRadians of anchorPhase (shortest arc on the circle).
*/
func (eigen *EigenMode) IsGeometricallyClosed(values []primitive.Value, anchorPhase float64) bool {
	return eigen.IsGeometricallyClosedWithEpsilon(values, anchorPhase, DefaultToroidalClosureRadians)
}

/*
IsGeometricallyClosedWithEpsilon uses an explicit angular tolerance (radians).
*/
func (eigen *EigenMode) IsGeometricallyClosedWithEpsilon(
	values []primitive.Value,
	anchorPhase float64,
	epsilonRadians float64,
) bool {
	if len(values) == 0 {
		return false
	}

	centerPhase, _ := eigen.WeightedCircularMean(values)
	phaseDiff := math.Abs(centerPhase - anchorPhase)

	if phaseDiff > math.Pi {
		phaseDiff = 2*math.Pi - phaseDiff
	}

	return phaseDiff < epsilonRadians
}

/*
EigenError represents legacy eigendecomposition failure.
Analytical EigenMode does not emit it; kept for API compatibility.
*/
type EigenError string

const (
	EigenErrorFactorizeFailed EigenError = "eig.Factorize failed"
)

/*
Error implements the error interface.
*/
func (err EigenError) Error() string {
	return string(err)
}
