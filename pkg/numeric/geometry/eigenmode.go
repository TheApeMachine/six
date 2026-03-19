package geometry

import (
	"math"

	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	config "github.com/theapemachine/six/pkg/system/core"
)

/*
EigenMode maps values to toroidal phase (Theta, Phi) from their bit distribution.
Value-native and analytical: no transition matrices; derives phase from
prime activations over GF(257).

Theta: Circular mean of active prime angles (2π·idx/257). Matches RotateX translation.
Phi: 2π·ActiveCount/257 — scales popcount into [0,2π).
*/
type EigenMode struct {
	Trained bool // Always true, kept for interface compatibility
}

/*
eigenModeOpts configures EigenMode at construction. Used for interface compatibility.
*/
type eigenModeOpts func(*EigenMode)

/*
NewEigenMode creates a new stateless, value-native phase evaluator.
No training is required; the analytical model is instantly ready.
*/
func NewEigenMode(opts ...eigenModeOpts) *EigenMode {
	ei := &EigenMode{
		Trained: true,
	}

	for _, opt := range opts {
		opt(ei)
	}

	return ei
}

/*
BuildMultiScaleCooccurrence is a no-op for the analytical EigenMode.
Legacy implementations required building massive 256x256 transition matrices
and running eigendecomposition. The analytical model is instantly ready.
*/
func (ei *EigenMode) BuildMultiScaleCooccurrence(values []primitive.Value) error {
	ei.Trained = true
	return nil
}

/*
PhaseForValue maps a single value to (theta, phi) purely through its
intrinsic geometric bit distribution over GF(257).
*/
func (ei *EigenMode) PhaseForValue(c *primitive.Value) (theta, phi float64) {
	indices := primitive.ValuePrimeIndices(c)
	var sinSum, cosSum float64

	// Theta: circular mean of angles 2π·idx/257 for active prime indices.
	// RotateX on the value translates Theta around the torus.
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

	// Phi: ActiveCount scaled into [0, 2π) for toroidal phase.
	phi = 2 * math.Pi * float64(c.ActiveCount()) / 257.0

	return theta, phi
}

/*
SeqToroidalMeanPhase returns the circular means of the intrinsic phases
for a sequence of values.
*/
func (ei *EigenMode) SeqToroidalMeanPhase(values []primitive.Value) (theta, phi float64) {
	n := len(values)
	if n == 0 {
		return 0, 0
	}

	var sinTSum, cosTSum float64
	var sinPSum, cosPSum float64

	for i := range values {
		theta, phi := ei.PhaseForValue(&values[i])
		sinTSum += math.Sin(theta)
		cosTSum += math.Cos(theta)
		sinPSum += math.Sin(phi)
		cosPSum += math.Cos(phi)
	}

	return math.Atan2(sinTSum, cosTSum), math.Atan2(sinPSum, cosPSum)
}

/*
WeightedCircularMean computes the circular mean of Theta phases, weighted by ActiveCount per value.
Returns (phase, concentration) where concentration = |R|/wSum and R is the resultant vector.
*/
func (ei *EigenMode) WeightedCircularMean(values []primitive.Value) (phase float64, concentration float64) {
	if len(values) == 0 {
		return 0, 0
	}
	var sinSum, cosSum, wSum float64
	for i := range values {
		theta, _ := ei.PhaseForValue(&values[i])
		weight := float64(values[i].ActiveCount())
		if weight <= 0 {
			weight = 1.0 // safeguard zero-density
		}
		sinSum += weight * math.Sin(theta)
		cosSum += weight * math.Cos(theta)
		wSum += weight
	}
	if wSum == 0 {
		return 0, 0
	}
	phase = math.Atan2(sinSum, cosSum)
	concentration = math.Sqrt(sinSum*sinSum+cosSum*cosSum) / wSum
	return
}

/*
IsGeometricallyClosed returns true when the sequence's weighted circular mean phase
is within config.Numeric.ShannonCapacity of anchorPhase (shortest path around the torus).
*/
func (ei *EigenMode) IsGeometricallyClosed(values []primitive.Value, anchorPhase float64) bool {
	if len(values) == 0 {
		return false
	}

	cPhase, _ := ei.WeightedCircularMean(values)
	phaseDiff := math.Abs(cPhase - anchorPhase)

	// Shortest path around torus boundary
	if phaseDiff > math.Pi {
		phaseDiff = 2*math.Pi - phaseDiff
	}

	return phaseDiff < config.Numeric.ShannonCapacity
}

/*
EigenError represents legacy eigendecomposition failure.
Preserved for interface compatibility; analytical EigenMode does not emit it.
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
