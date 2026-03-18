/*
package dmt implements metrics tracking for the radix tree system.
LatencyTracker maintains a rolling window of operation latencies.
*/
package dmt

import (
	"sync"
	"time"
)

/*
LatencyTracker maintains a rolling window of operation latencies.
It provides thread-safe tracking of operation durations and calculates
average latency over the window period.
*/
type LatencyTracker struct {
	window []time.Duration
	mu     sync.RWMutex
	size   int
	pos    int
}

/*
NewLatencyTracker creates a new latency tracker with the specified window size.
The window size determines how many measurements are kept for calculating
the rolling average.
*/
func NewLatencyTracker(size int) *LatencyTracker {
	return &LatencyTracker{
		window: make([]time.Duration, size),
		size:   size,
	}
}

/*
RecordLatency adds a new latency measurement to the tracker.
It stores the duration in the rolling window and updates the position
for the next measurement.
*/
func (lt *LatencyTracker) RecordLatency(d time.Duration) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	lt.window[lt.pos] = d
	lt.pos = (lt.pos + 1) % lt.size
}

/*
AverageLatency returns the average latency over the window.
It calculates the mean of all non-zero measurements in the window,
providing a rolling average of operation latencies.
*/
func (lt *LatencyTracker) AverageLatency() time.Duration {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	var sum time.Duration
	count := 0
	for _, d := range lt.window {
		if d > 0 {
			sum += d
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / time.Duration(count)
}
