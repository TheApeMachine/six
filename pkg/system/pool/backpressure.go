package pool

import (
	"sync"
	"time"
)

/*
BackPressureRegulator limits job intake when the system is under
pressure (queue deep, latency high).
*/
type BackPressureRegulator struct {
	mu sync.RWMutex

	maxQueueSize      int
	targetProcessTime time.Duration
	currentPressure   float64
	metrics           *Metrics
}

func NewBackPressureRegulator(
	maxQueueSize int,
	targetProcessTime,
	pressureWindow time.Duration,
) *BackPressureRegulator {
	if maxQueueSize <= 0 {
		maxQueueSize = 1
	}
	if targetProcessTime <= 0 {
		targetProcessTime = time.Millisecond
	}
	return &BackPressureRegulator{
		maxQueueSize:      maxQueueSize,
		targetProcessTime: targetProcessTime,
		currentPressure:   0.0,
	}
}

/*
Observe updates the regulator with current metrics and recomputes pressure.
*/
func (bp *BackPressureRegulator) Observe(metrics *Metrics) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	bp.metrics = metrics
	bp.updatePressure()
}

/*
Limit returns true when pressure exceeds the threshold and intake should be limited.
*/
func (bp *BackPressureRegulator) Limit() bool {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return bp.currentPressure >= 0.8
}

/*
Renormalize decreases pressure when queue and latency are below targets.
*/
func (bp *BackPressureRegulator) Renormalize() {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	if bp.metrics != nil &&
		bp.metrics.JobQueueSize < bp.maxQueueSize/2 &&
		bp.metrics.AverageJobLatency < bp.targetProcessTime {
		bp.currentPressure = max(0.0, bp.currentPressure-0.1)
	}
}

/*
updatePressure computes current pressure from queue size and latency.
*/
func (bp *BackPressureRegulator) updatePressure() {
	if bp.metrics == nil {
		return
	}

	queuePressure := float64(bp.metrics.JobQueueSize) / float64(bp.maxQueueSize)

	timingPressure := 0.0
	if bp.metrics.AverageJobLatency > 0 {
		timingPressure = float64(bp.metrics.AverageJobLatency) / float64(bp.targetProcessTime)
	}

	bp.currentPressure = min(1.0, max(0.0, queuePressure*0.6+timingPressure*0.4))
}

/*
GetPressure returns the current pressure level (0.0–1.0).
*/
func (bp *BackPressureRegulator) GetPressure() float64 {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return bp.currentPressure
}
