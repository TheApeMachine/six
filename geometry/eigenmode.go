package geometry

import (
	"math"

	"github.com/theapemachine/six/data"
)

/*
EigenMode maps arbitrary chord structures to a global toroidal phase space (Theta, Phi).
Unlike legacy Markov transition matrices, this implementation is entirely
chord-native and analytical: it derives the global shape of the sequence
from the intrinsic Fourier transform across the 257-dimensional GF(257) substrate.

Theta: Circular mean of prime activations. Perfectly mirrors topological translation (RotateX).
Phi: Spectral density moment (information content).
*/
type EigenMode struct {
	Trained bool // Always true, kept for interface compatibility
}

/*
eigenModeOpts configures EigenMode at construction. Used for interface compatibility.
*/
type eigenModeOpts func(*EigenMode)

/*
NewEigenMode creates a new stateless, chord-native phase evaluator.
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
func (ei *EigenMode) BuildMultiScaleCooccurrence(chords []data.Chord) error {
	ei.Trained = true
	return nil
}

/*
PhaseForChord maps a single chord to (theta, phi) purely through its
intrinsic geometric bit distribution over GF(257).
*/
func (ei *EigenMode) PhaseForChord(c *data.Chord) (theta, phi float64) {
	indices := data.ChordPrimeIndices(c)
	var sinSum, cosSum float64

	// Theta: the circular mean of active subsets in the GF(257) space.
	// This captures structural orientation. A topological ShiftX on the chord
	// precisely translates this Theta phase around the torus.
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

	// Phi: captures information density spread over the full 2π cycle.
	// We normalize the ActiveCount against the maximum expected typical
	// working memory density (for BaseChords it's ~5, here scaled out against
	// the 257 parameterization base, effectively becoming a density phase).
	phi = 2 * math.Pi * float64(c.ActiveCount()) / 257.0

	return theta, phi
}

/*
SeqToroidalMeanPhase returns the circular means of the intrinsic phases
for a sequence of chords.
*/
func (ei *EigenMode) SeqToroidalMeanPhase(chords []data.Chord) (theta, phi float64) {
	n := len(chords)
	if n == 0 {
		return 0, 0
	}

	var sinTSum, cosTSum float64
	var sinPSum, cosPSum float64

	for i := range chords {
		t, p := ei.PhaseForChord(&chords[i])
		sinTSum += math.Sin(t)
		cosTSum += math.Cos(t)
		sinPSum += math.Sin(p)
		cosPSum += math.Cos(p)
	}

	return math.Atan2(sinTSum, cosTSum), math.Atan2(sinPSum, cosPSum)
}

/*
WeightedCircularMean computes the weighted circular mean and concentration over PhaseTheta
for a sequence of chords. Weight is driven by chord structural density.
*/
func (ei *EigenMode) WeightedCircularMean(chords []data.Chord) (phase float64, concentration float64) {
	if len(chords) == 0 {
		return 0, 0
	}
	var sinSum, cosSum, wSum float64
	for i := range chords {
		t, _ := ei.PhaseForChord(&chords[i])
		w := float64(chords[i].ActiveCount())
		if w <= 0 {
			w = 1.0 // safeguard zero-density
		}
		sinSum += w * math.Sin(t)
		cosSum += w * math.Cos(t)
		wSum += w
	}
	if wSum == 0 {
		return 0, 0
	}
	phase = math.Atan2(sinSum, cosSum)
	concentration = math.Sqrt(sinSum*sinSum+cosSum*cosSum) / wSum
	return
}

/*
IsGeometricallyClosed verifies structural closure by checking if the sequence's
weighted mean phase trajectory closely returns to the expected anchor phase.
*/
func (ei *EigenMode) IsGeometricallyClosed(chords []data.Chord, anchorPhase float64) bool {
	if len(chords) == 0 {
		return false
	}

	cPhase, _ := ei.WeightedCircularMean(chords)
	phaseDiff := math.Abs(cPhase - anchorPhase)

	// Shortest path around torus boundary
	for phaseDiff > math.Pi {
		phaseDiff = 2*math.Pi - phaseDiff
	}

	return phaseDiff < 0.45
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
