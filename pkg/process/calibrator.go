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

type Calibrator struct {
	mu               sync.RWMutex
	sensitivityPop   float64
	sensitivityPhase float64
	window           *FastWindow
	phaseWindow      *FastWindow
}

type CalibratorOption func(*Calibrator)

func WithWindowSize(size int) CalibratorOption {
	return func(c *Calibrator) {
		c.window = NewFastWindow(size)
		c.phaseWindow = NewFastWindow(size)
	}
}

func NewCalibrator(opts ...CalibratorOption) *Calibrator {
	c := &Calibrator{
		sensitivityPop:   1.0,
		sensitivityPhase: 1.0,
		window:           NewFastWindow(config.Numeric.NSymbols),
		phaseWindow:      NewFastWindow(config.Numeric.NSymbols),
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

	targetDensity := config.Numeric.ShannonCapacity

	errorSignal := (targetDensity - meanDensity) / targetDensity

	lr := math.Max(stddev, 0.05)

	calibrator.sensitivityPop *= math.Exp(errorSignal * lr)
	calibrator.sensitivityPop = math.Max(0.01, math.Min(100.0, calibrator.sensitivityPop))

	calibrator.sensitivityPhase *= math.Exp(errorSignal * lr)
	calibrator.sensitivityPhase = math.Max(0.01, math.Min(100.0, calibrator.sensitivityPhase))
}

func (calibrator *Calibrator) ObservePhase(delta float64) {
	calibrator.mu.Lock()
	defer calibrator.mu.Unlock()

	if calibrator.phaseWindow == nil {
		return
	}

	calibrator.phaseWindow.Push(delta)
}

func (calibrator *Calibrator) DensityCeiling(fallback float64) float64 {
	calibrator.mu.RLock()
	defer calibrator.mu.RUnlock()

	return calibrator.dynamicLimit(calibrator.window, calibrator.sensitivityPop, fallback)
}

func (calibrator *Calibrator) PhaseLimit(fallback float64) float64 {
	calibrator.mu.RLock()
	defer calibrator.mu.RUnlock()

	return calibrator.dynamicLimit(calibrator.phaseWindow, calibrator.sensitivityPhase, fallback)
}

func (calibrator *Calibrator) dynamicLimit(window *FastWindow, sensitivity, fallback float64) float64 {
	if window == nil || !window.Warmed() {
		return fallback
	}

	mean, stddev := window.Stats()
	if mean <= 0 {
		return fallback
	}

	limit := mean * sensitivity
	if stddev > 0 {
		limit = mean + sensitivity*stddev
	}

	if limit <= 0 {
		return fallback
	}

	return math.Min(fallback, limit)
}
