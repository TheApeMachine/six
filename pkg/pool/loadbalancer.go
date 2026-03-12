package pool

import (
	"errors"
	"sync"
	"time"
)

// ErrNoAvailableWorkers is returned when no worker has capacity.
var ErrNoAvailableWorkers = errors.New("no workers available to process job")

// LoadBalancer distributes work across workers based on load and latency.
type LoadBalancer struct {
	mu sync.RWMutex

	workerLoads    map[int]float64
	workerLatency  map[int]time.Duration
	workerCapacity map[int]int
	activeWorkers  int
	metrics        *Metrics
}

// NewLoadBalancer creates a balancer for workerCount workers
// with the given per-worker capacity.
func NewLoadBalancer(workerCount, workerCapacity int) *LoadBalancer {
	lb := &LoadBalancer{
		workerLoads:    make(map[int]float64),
		workerLatency:  make(map[int]time.Duration),
		workerCapacity: make(map[int]int),
		activeWorkers:  workerCount,
	}

	for i := 0; i < workerCount; i++ {
		lb.workerCapacity[i] = workerCapacity
		lb.workerLoads[i] = 0.0
		lb.workerLatency[i] = 0
	}

	return lb
}

func (lb *LoadBalancer) Observe(metrics *Metrics) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.metrics = metrics
	lb.updateWorkerStats()
}

func (lb *LoadBalancer) Limit() bool {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	for i := 0; i < lb.activeWorkers; i++ {
		if lb.workerLoads[i] < float64(lb.workerCapacity[i]) {
			return false
		}
	}
	return true
}

func (lb *LoadBalancer) Renormalize() {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	for i := 0; i < lb.activeWorkers; i++ {
		if lb.workerLoads[i] > float64(lb.workerCapacity[i]) {
			lb.workerLoads[i] = float64(lb.workerCapacity[i])
		}
	}
}

// SelectWorker returns the worker ID with the lowest load.
func (lb *LoadBalancer) SelectWorker() (int, error) {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	selected := -1

	for i := 0; i < lb.activeWorkers; i++ {
		if lb.workerLoads[i] >= float64(lb.workerCapacity[i]) {
			continue
		}

		if selected == -1 {
			selected = i
			continue
		}

		if lb.workerLoads[i] < lb.workerLoads[selected] {
			selected = i
		} else if lb.workerLoads[i] == lb.workerLoads[selected] {
			if lb.workerLatency[selected] == 0 ||
				(lb.workerLatency[i] > 0 && lb.workerLatency[i] < lb.workerLatency[selected]) {
				selected = i
			}
		}
	}

	if selected == -1 {
		return -1, ErrNoAvailableWorkers
	}

	return selected, nil
}

// RecordJobStart increments the worker's active load.
func (lb *LoadBalancer) RecordJobStart(workerID int) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if workerID >= 0 && workerID < lb.activeWorkers {
		lb.workerLoads[workerID]++
	}
}

// RecordJobComplete decrements load and updates latency.
func (lb *LoadBalancer) RecordJobComplete(workerID int, duration time.Duration) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if workerID >= 0 && workerID < lb.activeWorkers {
		lb.workerLoads[workerID]--
		if lb.workerLoads[workerID] < 0 {
			lb.workerLoads[workerID] = 0
		}

		if lb.workerLatency[workerID] == 0 {
			lb.workerLatency[workerID] = duration
		} else {
			lb.workerLatency[workerID] = (lb.workerLatency[workerID]*4 + duration) / 5
		}
	}
}

func (lb *LoadBalancer) updateWorkerStats() {
	if lb.metrics == nil {
		return
	}

	if lb.metrics.WorkerCount != lb.activeWorkers {
		newCount := lb.metrics.WorkerCount
		if newCount > lb.activeWorkers {
			for i := lb.activeWorkers; i < newCount; i++ {
				lb.workerCapacity[i] = lb.workerCapacity[0]
				lb.workerLoads[i] = 0.0
				lb.workerLatency[i] = 0
			}
		}
		lb.activeWorkers = newCount
	}
}
