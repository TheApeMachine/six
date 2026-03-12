package process

import (
	"math"
	"sync"

	config "github.com/theapemachine/six/pkg/core"
)

const (
	EventLowVarianceFlux = iota // 5-Cycle
	EventDensitySpike           // 3-Cycle
	EventDensityTrough          // Inverse 3-Cycle
	EventPhaseInversion         // Double Transposition
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

type CalibratorOption func(*Calibrator)

func WithWindowSize(size int) CalibratorOption {
	return func(c *Calibrator) {
		c.window = NewFastWindow(size)
	}
}

/*
NewCalibrator creates a dynamic calibrator that learns boundaries strictly through feedback.
*/
func NewCalibrator(opts ...CalibratorOption) *Calibrator {
	c := &Calibrator{
		sensitivityPop:   1.0,
		sensitivityPhase: 1.0,
		window:           NewFastWindow(config.Numeric.NSymbols),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
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
FeedbackChunk adjusts the calibrator based on the average emitted chunk density.
This approach is modality-agnostic: it aims to maximize informational chunk
length without hitting the Shannon Ceiling.
*/
func (calibrator *Calibrator) FeedbackChunk(length int, density float64) {
	calibrator.mu.Lock()
	defer calibrator.mu.Unlock()

	calibrator.window.Push(density)

	if !calibrator.window.Warmed() {
		return
	}

	meanDensity, stddev := calibrator.window.Stats()
	if meanDensity == 0 {
		return
	}

	// We optimize for a target density just below the global ShannonCapacity
	targetDensity := config.Numeric.ShannonCapacity

	// Proportional control loop based on density
	errorSignal := (targetDensity - meanDensity) / targetDensity

	// Apply a minimum learning rate so the calibrator can't freeze when
	// variance collapses (e.g. all chunks are tiny and equally sparse).
	lr := math.Max(stddev, 0.05)

	calibrator.sensitivityPop *= math.Exp(errorSignal * lr)
	calibrator.sensitivityPop = math.Max(0.01, math.Min(100.0, calibrator.sensitivityPop))

	calibrator.sensitivityPhase *= math.Exp(errorSignal * lr)
	calibrator.sensitivityPhase = math.Max(0.01, math.Min(100.0, calibrator.sensitivityPhase))
}
