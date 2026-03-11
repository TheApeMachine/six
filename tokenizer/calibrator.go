package tokenizer

import (
	"math"
	"sync"

	"github.com/theapemachine/six/geometry"
)

/*
Calibrator holds BIC penalty and density thresholds for Sequencer boundary detection.
sensitivityPop scales the MDL penalty; sensitivityPhase for phase-based signals.
*/
type Calibrator struct {
	mu               sync.RWMutex
	targetDensityMin float64
	targetDensityMax float64
	sensitivityPop   float64
	sensitivityPhase float64

	window       *FastWindow
	entropyFloor float64
}

const (
	// sensitivityDecayFactor prevents over-sensitivity when data is too quiet.
	sensitivityDecayFactor = 0.95
	// sensitivityGrowFactor forces higher penalties on highly volatile streams.
	sensitivityGrowFactor = 1.05
	// sensitivityClampMin prevents the penalty from disappearing entirely.
	sensitivityClampMin = 0.05
	// sensitivityClampMax prevents the penalty from suppressing all splits.
	sensitivityClampMax = 20.0
	// volatilityMultiplier defines the delta from entropyFloor to trigger growth.
	volatilityMultiplier = 3.0
)

/*
NewCalibrator creates a Calibrator with phi-based defaults: targetDensity 1/phi³..1/phi²,
sensitivityPop=1, sensitivityPhase=phi.
*/
func NewCalibrator() *Calibrator {
	phi := (1.0 + math.Sqrt(5.0)) / 2.0

	return &Calibrator{
		targetDensityMin: 1.0 / math.Pow(phi, 3),
		targetDensityMax: 1.0 / math.Pow(phi, 2),

		// BIC penalty scale: 1.0 yields practical boundary detection on typical
		// byte streams; phi (≈1.618) over-penalizes and suppresses splits.
		sensitivityPop:   1.0,
		sensitivityPhase: phi,
		window:           NewFastWindow(100),
		entropyFloor:     0.05, // quiet ticks threshold
	}
}

func (c *Calibrator) TargetDensityMin() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.targetDensityMin
}

func (c *Calibrator) TargetDensityMax() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.targetDensityMax
}

func (c *Calibrator) SensitivityPop() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sensitivityPop
}

func (c *Calibrator) SensitivityPhase() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sensitivityPhase
}

func (c *Calibrator) SetSensitivityPop(v float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sensitivityPop = v
}

func (c *Calibrator) SetSensitivityPhase(v float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sensitivityPhase = v
}

/*
Recalibrate adjusts the scale of sensitivity based on recent event frequency.
*/
func (calibrator *Calibrator) Recalibrate(events []int) {
	calibrator.mu.Lock()
	defer calibrator.mu.Unlock()

	// Track whether a boundary event occurred
	hasBoundary := false
	for _, e := range events {
		if e == geometry.EventDensitySpike || e == geometry.EventDensityTrough {
			hasBoundary = true
			break
		}
	}

	if hasBoundary {
		calibrator.window.Push(1.0)
	} else {
		calibrator.window.Push(0.0)
	}

	if !calibrator.window.Warmed() {
		return
	}

	mean, _ := calibrator.window.Stats()

	// Adjust sensitivity using adaptive feedback loop.
	// We use decay/grow factors to maintain stability vs responsiveness.
	if mean < calibrator.entropyFloor {
		// Stuck in a monotonous region, decrease sensitivity.
		calibrator.sensitivityPop *= sensitivityDecayFactor
		if calibrator.sensitivityPop < sensitivityClampMin {
			calibrator.sensitivityPop = sensitivityClampMin
		}
	} else if mean > calibrator.entropyFloor*volatilityMultiplier {
		// Highly volatile data, increase penalty to avoid noise splits.
		calibrator.sensitivityPop *= sensitivityGrowFactor
		if calibrator.sensitivityPop > sensitivityClampMax {
			calibrator.sensitivityPop = sensitivityClampMax
		}
	}
}
