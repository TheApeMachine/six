package process

import (
	"math"
	"sync"

	"github.com/theapemachine/six/geometry"
)

/*
Calibrator dynamically holds BIC penalty and density thresholds for Sequencer boundary detection.
It uses dynamic feedback derived from its sliding window to adjust penalties.
*/
type Calibrator struct {
	mu               sync.RWMutex
	sensitivityPop   float64
	sensitivityPhase float64
	window           *FastWindow
}

/*
NewCalibrator creates a dynamic calibrator that learns boundaries strictly through feedback.
*/
func NewCalibrator() *Calibrator {
	return &Calibrator{
		sensitivityPop:   1.0,
		sensitivityPhase: 1.0,
		window:           NewFastWindow(128),
	}
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
It uses pure dynamic feedback based on the moving average compared to the standard deviation.
*/
func (calibrator *Calibrator) Recalibrate(events []int) {
	calibrator.mu.Lock()
	defer calibrator.mu.Unlock()

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

	mean, stddev := calibrator.window.Stats()
	if mean == 0 || stddev == 0 {
		calibrator.sensitivityPop = math.Max(calibrator.sensitivityPop*0.9, 0.01)
		return
	}

	// We use standard deviation as a legitimate dynamic target for the boundary rate.
	// If the boundary rate (mean) exceeds the signal's volatility (stddev),
	// we are splitting too much (noise), so we increase the penalty.
	errorRate := mean - stddev

	// Feedback driven exponential adjustment
	calibrator.sensitivityPop *= math.Exp(errorRate)
	calibrator.sensitivityPop = math.Max(0.01, math.Min(100.0, calibrator.sensitivityPop))
}
