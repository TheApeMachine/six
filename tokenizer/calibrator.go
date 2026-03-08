package tokenizer

import (
	"math"
	"sync"
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
}

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
Recalibrate is a placeholder for future dynamic threshold adjustment. No-op.
*/
func (calibrator *Calibrator) Recalibrate() {
	calibrator.mu.Lock()
	defer calibrator.mu.Unlock()
}
