package pool

import (
	"math"
	"sort"
	"sync"
	"time"
)

type tDigestCentroid struct {
	mean  float64
	count int64
}

/*
Metrics collects pool-level performance statistics including
t-digest percentile tracking for latency.
*/
type Metrics struct {
	mu                   sync.RWMutex
	WorkerCount          int
	JobQueueSize         int
	IdleWorkers          int
	LastScale            time.Time
	ErrorRates           map[string]float64
	TotalJobTime         time.Duration
	JobCount             int64
	CircuitBreakerStates map[string]CircuitState

	AverageJobLatency   time.Duration
	P95JobLatency       time.Duration
	P99JobLatency       time.Duration
	JobSuccessRate      float64
	QueueWaitTime       time.Duration
	ResourceUtilization float64

	RateLimitHits      int64
	ThrottledJobs      int64
	SchedulingFailures int64
	FailureCount       int64

	centroids    []tDigestCentroid
	compression  float64
	totalWeight  int64
	maxCentroids int
}

/*
NewMetrics initialises a Metrics instance with sensible defaults.
*/
func NewMetrics() *Metrics {
	return &Metrics{
		ErrorRates:           make(map[string]float64),
		CircuitBreakerStates: make(map[string]CircuitState),
		compression:          100,
		maxCentroids:         100,
		centroids:            make([]tDigestCentroid, 0, 100),
		JobSuccessRate:       1.0,
	}
}

/*
NewMetricsForExportTest builds a Metrics snapshot used to exercise ExportMetrics
without tests reaching into internal fields.
*/
func NewMetricsForExportTest(
	workerCount, idleWorkers, jobQueueSize int,
	jobSuccessRate float64,
	avgLatency, p95Latency, p99Latency time.Duration,
	resourceUtilization float64,
) *Metrics {
	m := NewMetrics()

	m.mu.Lock()
	m.WorkerCount = workerCount
	m.IdleWorkers = idleWorkers
	m.JobQueueSize = jobQueueSize
	m.JobSuccessRate = jobSuccessRate
	m.AverageJobLatency = avgLatency
	m.P95JobLatency = p95Latency
	m.P99JobLatency = p99Latency
	m.ResourceUtilization = resourceUtilization
	m.mu.Unlock()

	return m
}

/*
SetMaxCentroids sets the t-digest centroid cap (test and tuning hook).
This method acquires m.mu and is safe for concurrent use.
*/
func (m *Metrics) SetMaxCentroids(maxCentroids int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxCentroids = maxCentroids
}

/*
SetCompression sets the t-digest compression factor (test and tuning hook).
This method acquires m.mu and is safe for concurrent use.
*/
func (m *Metrics) SetCompression(compression int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.compression = float64(compression)
}

/*
CentroidCount returns the current t-digest centroid count.
*/
func (m *Metrics) CentroidCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.centroids)
}

/*
TotalWeight returns the number of samples fed into the latency digest.
*/
func (m *Metrics) TotalWeight() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return int(m.totalWeight)
}

/*
RecordJobExecution records execution time and outcome.
*/
func (m *Metrics) RecordJobExecution(startTime time.Time, success bool) {
	duration := time.Since(startTime)

	m.mu.Lock()
	m.TotalJobTime += duration
	m.JobCount++
	if success {
		m.JobSuccessRate = float64(m.JobCount-m.FailureCount) / float64(m.JobCount)
	}
	m.updateLatencyPercentilesLocked(duration)
	m.mu.Unlock()
}

/*
RecordJobSuccess records a successful job with its latency.
*/
func (m *Metrics) RecordJobSuccess(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.JobCount++
	m.TotalJobTime += latency

	if m.JobCount > 0 {
		m.AverageJobLatency = time.Duration(int64(m.TotalJobTime) / m.JobCount)
		m.JobSuccessRate = float64(m.JobCount-m.FailureCount) / float64(m.JobCount)
	}

	m.updateLatencyPercentilesLocked(latency)
}

/*
RecordJobFailure increments the failure counter.
*/
func (m *Metrics) RecordJobFailure() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FailureCount++

	if m.JobCount > 0 {
		m.JobSuccessRate = float64(m.JobCount-m.FailureCount) / float64(m.JobCount)
	} else {
		m.JobSuccessRate = 0.0
	}
}

/*
ExportMetrics returns a snapshot for external consumption.
*/
func (m *Metrics) ExportMetrics() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]any{
		"worker_count":         m.WorkerCount,
		"idle_workers":         m.IdleWorkers,
		"queue_size":           m.JobQueueSize,
		"success_rate":         m.JobSuccessRate,
		"avg_latency":          m.AverageJobLatency.Milliseconds(),
		"p95_latency":          m.P95JobLatency.Milliseconds(),
		"p99_latency":          m.P99JobLatency.Milliseconds(),
		"resource_utilization": m.ResourceUtilization,
	}
}

/*
updateLatencyPercentilesLocked adds a latency sample to the t-digest and updates percentiles.
Must be called with m.mu held.
*/
func (m *Metrics) updateLatencyPercentilesLocked(duration time.Duration) {
	if m.JobCount > 0 {
		m.AverageJobLatency = time.Duration(int64(m.TotalJobTime) / m.JobCount)
	}
	value := float64(duration.Milliseconds())
	m.totalWeight++

	if len(m.centroids) == 0 {
		m.centroids = append(m.centroids, tDigestCentroid{mean: value, count: 1})
		m.P95JobLatency = duration
		m.P99JobLatency = duration
		return
	}

	idx := sort.Search(len(m.centroids), func(i int) bool {
		return m.centroids[i].mean >= value
	})

	inserted := false

	// Identical samples should coalesce into the same centroid.
	if idx < len(m.centroids) && m.centroids[idx].mean == value {
		c := &m.centroids[idx]
		c.mean = (c.mean*float64(c.count) + value) / float64(c.count+1)
		c.count++
		inserted = true
	} else if idx > 0 && m.centroids[idx-1].mean == value {
		c := &m.centroids[idx-1]
		c.mean = (c.mean*float64(c.count) + value) / float64(c.count+1)
		c.count++
		inserted = true
	}

	if !inserted {
		q := m.calculateQuantile(value)
		maxWeight := int64(4 * m.compression * math.Min(q, 1-q))
		if maxWeight < 1 {
			maxWeight = 1
		}

		if idx < len(m.centroids) && m.centroids[idx].count <= maxWeight {
			c := &m.centroids[idx]
			c.mean = (c.mean*float64(c.count) + value) / float64(c.count+1)
			c.count++
			inserted = true
		} else if idx > 0 && m.centroids[idx-1].count <= maxWeight {
			c := &m.centroids[idx-1]
			c.mean = (c.mean*float64(c.count) + value) / float64(c.count+1)
			c.count++
			inserted = true
		}
	}

	if !inserted {
		newCentroid := tDigestCentroid{mean: value, count: 1}
		m.centroids = append(m.centroids, tDigestCentroid{})
		copy(m.centroids[idx+1:], m.centroids[idx:])
		m.centroids[idx] = newCentroid
	}

	if len(m.centroids) > m.maxCentroids {
		m.compress()
	}

	m.P95JobLatency = time.Duration(m.estimatePercentile(0.95)) * time.Millisecond
	m.P99JobLatency = time.Duration(m.estimatePercentile(0.99)) * time.Millisecond
}

/*
calculateQuantile returns the approximate rank of value in the t-digest.
*/
func (m *Metrics) calculateQuantile(value float64) float64 {
	if m.totalWeight == 0 {
		return 0.0
	}
	rank := 0.0
	for _, c := range m.centroids {
		if c.mean < value {
			rank += float64(c.count)
		}
	}
	return rank / float64(m.totalWeight)
}

/*
estimatePercentile interpolates the latency at the given quantile.
*/
func (m *Metrics) estimatePercentile(p float64) float64 {
	if len(m.centroids) == 0 {
		return 0
	}

	targetRank := p * float64(m.totalWeight)
	cumulative := 0.0

	for i, c := range m.centroids {
		cumulative += float64(c.count)
		if cumulative >= targetRank {
			if i > 0 {
				prev := m.centroids[i-1]
				prevCumulative := cumulative - float64(c.count)
				if c.count == 0 {
					return prev.mean
				}
				t := (targetRank - prevCumulative) / float64(c.count)
				return prev.mean + t*(c.mean-prev.mean)
			}
			return c.mean
		}
	}
	return m.centroids[len(m.centroids)-1].mean
}

/*
compress merges centroids to stay within maxCentroids.
*/
func (m *Metrics) compress() {
	if len(m.centroids) <= 1 {
		return
	}

	sort.Slice(m.centroids, func(i, j int) bool {
		return m.centroids[i].mean < m.centroids[j].mean
	})

	newCentroids := make([]tDigestCentroid, 0, m.maxCentroids)
	current := m.centroids[0]

	for i := 1; i < len(m.centroids); i++ {
		if current.count+m.centroids[i].count <= int64(m.compression) {
			totalCount := current.count + m.centroids[i].count
			current.mean = (current.mean*float64(current.count) +
				m.centroids[i].mean*float64(m.centroids[i].count)) /
				float64(totalCount)
			current.count = totalCount
		} else {
			newCentroids = append(newCentroids, current)
			current = m.centroids[i]
		}
	}
	newCentroids = append(newCentroids, current)
	m.centroids = newCentroids
}
