package pool

import (
	"errors"
	"sync"
	"time"
)

/*
ErrNoAvailableWorkers is returned when no worker has capacity.
*/
var ErrNoAvailableWorkers = errors.New("no workers available to process job")

/*
LoadBalancer distributes work across workers based on load and latency.
*/
type LoadBalancer struct {
	mu sync.RWMutex

	workerLoads     map[int]float64
	workerLatency   map[int]time.Duration
	workerCapacity  map[int]int
	activeWorkers   int
	defaultCapacity int
	metrics         *Metrics
}

/*
NewLoadBalancer creates a balancer for workerCount workers
with the given per-worker capacity.
*/
func NewLoadBalancer(workerCount, workerCapacity int) *LoadBalancer {
	capacity := workerCapacity
	if capacity < 1 {
		capacity = 1
	}
	lb := &LoadBalancer{
		workerLoads:     make(map[int]float64),
		workerLatency:   make(map[int]time.Duration),
		workerCapacity:  make(map[int]int),
		activeWorkers:   workerCount,
		defaultCapacity: capacity,
	}

	for i := 0; i < workerCount; i++ {
		lb.workerCapacity[i] = capacity
		lb.workerLoads[i] = 0.0
		lb.workerLatency[i] = 0
	}

	return lb
}

/*
Observe updates the balancer with current metrics and worker stats.
*/
func (lb *LoadBalancer) Observe(metrics *Metrics) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.metrics = metrics
	lb.updateWorkerStats()
}

/*
Limit returns true when all workers are at capacity.
*/
func (lb *LoadBalancer) Limit() bool {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	for workerID := range lb.workerLoads {
		if lb.workerLoads[workerID] < float64(lb.workerCapacity[workerID]) {
			return false
		}
	}
	return true
}

/*
Renormalize clamps worker loads to their capacity.
*/
func (lb *LoadBalancer) Renormalize() {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	for workerID := range lb.workerLoads {
		if lb.workerLoads[workerID] > float64(lb.workerCapacity[workerID]) {
			lb.workerLoads[workerID] = float64(lb.workerCapacity[workerID])
		}
	}
}

/*
SelectWorker returns the worker ID with the lowest load.
*/
func (lb *LoadBalancer) SelectWorker() (int, error) {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	selected := -1

	for workerID := range lb.workerLoads {
		if lb.workerLoads[workerID] >= float64(lb.workerCapacity[workerID]) {
			continue
		}

		if selected == -1 {
			selected = workerID
			continue
		}

		if lb.workerLoads[workerID] < lb.workerLoads[selected] {
			selected = workerID
		} else if lb.workerLoads[workerID] == lb.workerLoads[selected] {
			if lb.workerLatency[selected] == 0 ||
				(lb.workerLatency[workerID] > 0 && lb.workerLatency[workerID] < lb.workerLatency[selected]) {
				selected = workerID
			}
		}
	}

	if selected == -1 {
		return -1, ErrNoAvailableWorkers
	}

	return selected, nil
}

/*
RecordJobStart increments the worker's active load.
*/
func (lb *LoadBalancer) RecordJobStart(workerID int) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if _, ok := lb.workerLoads[workerID]; ok {
		lb.workerLoads[workerID]++
	}
}

/*
RecordJobComplete decrements load and updates latency.
*/
func (lb *LoadBalancer) RecordJobComplete(workerID int, duration time.Duration) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if _, ok := lb.workerLoads[workerID]; ok {
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

/*
updateWorkerStats syncs worker capacity from metrics and adds new workers if needed.
*/
func (lb *LoadBalancer) updateWorkerStats() {
	if lb.metrics == nil {
		return
	}

	if lb.metrics.WorkerCount != lb.activeWorkers {
		newCount := lb.metrics.WorkerCount
		if newCount > lb.activeWorkers {
			defaultCap := lb.defaultCapacity
			if defaultCap < 1 {
				for _, cap := range lb.workerCapacity {
					if cap > 0 {
						defaultCap = cap
						break
					}
				}
				if defaultCap < 1 {
					defaultCap = 1
				}
			}
			for i := lb.activeWorkers; i < newCount; i++ {
				lb.workerCapacity[i] = defaultCap
				lb.workerLoads[i] = 0.0
				lb.workerLatency[i] = 0
			}
		}
		lb.activeWorkers = newCount
	}
}
