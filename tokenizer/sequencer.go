package tokenizer

import (
	"math"

	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

/*
Sequencer is a mechanism that splits an incoming data stream into sequences
based on the topological properties of the data.
*/
type Sequencer struct {
	calibrator *Calibrator
	eigen      *geometry.EigenMode
	phi        float64
	phiFast    float64
	phiMed     float64
	phiSlow    float64
	emaAlpha   float64

	// Welford variance accumulators
	emaPop        float64
	emaPhase      float64
	popMean       float64
	popM2         float64
	phaseMean     float64
	phaseM2       float64
	count         float64
	coherenceTime int

	tokens []Token
}

func NewSequencer(calibrator *Calibrator) *Sequencer {
	phi := (1.0 + math.Sqrt(5.0)) / 2.0

	return &Sequencer{
		calibrator: calibrator,
		eigen:      geometry.NewEigenMode(),
		phi:        phi,
		phiFast:    math.Pow(phi, -9), // ~0.013
		phiMed:     math.Pow(phi, -6), // ~0.055
		phiSlow:    math.Pow(phi, -5), // ~0.090
		emaAlpha:   math.Pow(phi, -3), // ~0.236
	}
}

func (sequencer *Sequencer) Analyze(pos int, current data.Chord) (bool, []int) {
	pop, phase := sequencer.measure(pos, current)
	deltaPop, deltaPhase := sequencer.trackVariance(pop, phase)
	popThresh, phaseThresh := sequencer.deriveThresholds()

	var events []int

	topoThresh := math.Pi / config.Numeric.FrequencySpread
	isTopologicalBoundary := deltaPhase > topoThresh

	if deltaPop > popThresh {
		if pop > sequencer.emaPop {
			events = append(events, geometry.EventDensitySpike)
		} else {
			events = append(events, geometry.EventDensityTrough)
		}
	}

	if isTopologicalBoundary || deltaPhase > phaseThresh {
		events = append(events, geometry.EventPhaseInversion)
	}

	if deltaPhase < phaseThresh {
		sequencer.coherenceTime++
	}

	if len(events) > 0 {
		sequencer.calibrate(current)
		sequencer.coherenceTime = 0
	} else if sequencer.coherenceTime > 10 {
		events = append(events, geometry.EventLowVarianceFlux)
		sequencer.coherenceTime = 0
	}

	sequencer.syncCalibration()

	return len(events) > 0, events
}

/*
measure extracts the geometric population count and spatial phase
from the current token's chord representation.

Phase is derived from ChordBin — a deterministic structural hash of the
chord's bit pattern mapped to [0, 2π). If the EigenMode has been trained
(via BuildMultiScaleCooccurrence), its co-occurrence-informed phases are
used instead. This gives immediate phase signal from the first byte while
preserving the upgrade path to trained eigenmodes.
*/
func (sequencer *Sequencer) measure(pos int, current data.Chord) (float64, float64) {
	pop := float64(current.ActiveCount())

	phase := 0.0
	if sequencer.eigen != nil && sequencer.eigen.Trained {
		theta, phi := sequencer.eigen.PhaseForChord(&current)
		phase = math.Sqrt(theta*theta + phi*phi)
	} else {
		// If EigenMode is untrained, use intrinsic ChordBin phase.
		// Scale to [0, 1) — not [0, 2π) — because ChordBin is a SimHash that
		// produces effectively random values for nearby bytes. A full 2π range
		// overwhelms the EMA and fires events on every byte.
		bin := data.ChordBin(&current)
		phase = float64(bin) / 256.0
	}

	if pos == 0 {
		sequencer.emaPop = pop
		sequencer.emaPhase = phase
	}

	return pop, phase
}

/*
trackVariance applies Golden Ratio exponential smoothing to recent measurements
and maintains a running Welford's online variance calculation to construct
concept-drift resistant boundaries.
*/
func (sequencer *Sequencer) trackVariance(pop, phase float64) (float64, float64) {
	deltaPop := math.Abs(pop - sequencer.emaPop)
	deltaPhase := math.Abs(phase - sequencer.emaPhase)

	sequencer.emaPop = (sequencer.emaPop * (1.0 - sequencer.emaAlpha)) + (pop * sequencer.emaAlpha)
	sequencer.emaPhase = (sequencer.emaPhase * (1.0 - sequencer.emaAlpha)) + (phase * sequencer.emaAlpha)

	sequencer.count++
	popDiff := deltaPop - sequencer.popMean
	sequencer.popMean += popDiff / sequencer.count
	sequencer.popM2 += popDiff * (deltaPop - sequencer.popMean)

	phaseDiff := deltaPhase - sequencer.phaseMean
	sequencer.phaseMean += phaseDiff / sequencer.count
	sequencer.phaseM2 += phaseDiff * (deltaPhase - sequencer.phaseMean)

	return deltaPop, deltaPhase
}

/*
deriveThresholds calculates the dynamic Z-score boundaries based on the
current signal variance and the shared calibrator sensitivity targets.
*/
func (sequencer *Sequencer) deriveThresholds() (float64, float64) {
	popStdDev := 0.0
	phaseStdDev := 0.0

	if sequencer.count > 1 {
		// Unbiased sample standard deviation: M2 / (count - 1)
		popStdDev = math.Sqrt(sequencer.popM2 / (sequencer.count - 1))
		phaseStdDev = math.Sqrt(sequencer.phaseM2 / (sequencer.count - 1))
	}

	// Read calibrator fields under read lock
	sensPop := sequencer.calibrator.SensitivityPop()
	sensPhase := sequencer.calibrator.SensitivityPhase()

	popThresh := sequencer.popMean + (popStdDev * sensPop)
	phaseThresh := sequencer.phaseMean + (phaseStdDev * sensPhase)

	return popThresh, phaseThresh
}

/*
calibrate applies high-frequency density feedback to adjust sensitivity
multipliers if the chunk boundaries are forming too sparse or too dense.
*/
func (sequencer *Sequencer) calibrate(current data.Chord) {
	density := float64(current.ActiveCount()) / float64(config.Numeric.ChordBlocks*64)

	// Read calibrator fields under read lock
	maxDensity := sequencer.calibrator.TargetDensityMax()
	minDensity := sequencer.calibrator.TargetDensityMin()

	if density > maxDensity {
		sequencer.thresholdMultiplier(-1)
	} else if density < minDensity {
		sequencer.thresholdMultiplier(1)
	}
}

/*
syncCalibration atomically merges the fast-loop local sensitivity adjustments
back to the globally shared calibrator state using a median smoothing factor.
*/
func (sequencer *Sequencer) syncCalibration() {
	sequencer.calibrator.mu.Lock()
	sequencer.calibrator.sensitivityPop = sequencer.syncBack(sequencer.calibrator.sensitivityPop, sequencer.phi)
	sequencer.calibrator.sensitivityPhase = sequencer.syncBack(sequencer.calibrator.sensitivityPhase, sequencer.phi)
	sequencer.calibrator.mu.Unlock()
}

func (sequencer *Sequencer) thresholdMultiplier(direction float64) {
	// Clamp the multiplier to a non-negative value to avoid sign-flipping
	multiplier := direction + sequencer.phiFast
	if multiplier < 0 {
		multiplier = sequencer.phiFast
	}

	sequencer.calibrator.mu.Lock()
	sequencer.calibrator.sensitivityPop *= multiplier
	sequencer.calibrator.mu.Unlock()
}

func (sequencer *Sequencer) syncBack(current float64, target float64) float64 {
	return (current*(1.0-sequencer.phiMed) + target*sequencer.phiMed)
}

/*
FeedbackRetrievalQuality acts as the slow, global supervisor for the tokenizer.
Should be called by BestFill downstream when retrieval performance deviates.
overDiscriminated (true) means we missed relevant matches (false negatives), implying
boundaries are too wide/chaotic.
underDiscriminated (true) means we matched unrelated things (false positives), implying
boundaries aren't detecting enough variance.
*/
func (sequencer *Sequencer) FeedbackRetrievalQuality(overDiscriminated, underDiscriminated bool) {
	sequencer.calibrator.mu.Lock()
	defer sequencer.calibrator.mu.Unlock()

	if overDiscriminated {
		// Over-discriminating means too rigidly separating; raise bases to ignore more noise
		sequencer.calibrator.sensitivityPop *= (1.0 + sequencer.phiSlow)
		sequencer.calibrator.sensitivityPhase *= (1.0 + sequencer.phiSlow)
	} else if underDiscriminated {
		// Under-discriminating means not seeing enough fine detail; lower bases to be more sensitive
		sequencer.calibrator.sensitivityPop *= (1.0 - sequencer.phiSlow)
		sequencer.calibrator.sensitivityPhase *= (1.0 - sequencer.phiSlow)
	}
}

/*
Phase returns the current exponential moving average of the phase (Angular Momentum),
and the dynamically derived variance threshold for use in continuous generation decay.
*/
func (sequencer *Sequencer) Phase() (float64, float64) {
	_, phaseThresh := sequencer.deriveThresholds()
	return sequencer.emaPhase, phaseThresh
}

/*
Phi returns the golden ratio scaling factor used by the Sequencer.
*/
func (sequencer *Sequencer) Phi() float64 {
	return sequencer.phi
}

func (sequencer *Sequencer) SetEigenMode(eigen *geometry.EigenMode) {
	if eigen == nil {
		sequencer.eigen = geometry.NewEigenMode()
		return
	}

	sequencer.eigen = eigen
}
