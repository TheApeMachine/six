package pool

import (
	"math"
	"time"

	"github.com/charmbracelet/log"
)

/*
Scaler dynamically adjusts the pool's worker count based on queue load.
*/
type Scaler struct {
	pool               *Pool
	minWorkers         int
	maxWorkers         int
	targetLoad         float64
	scaleUpThreshold   float64
	scaleDownThreshold float64
	cooldown           time.Duration
	lastScale          time.Time
}

/*
ScalerConfig defines thresholds and timing for auto-scaling.
*/
type ScalerConfig struct {
	TargetLoad         float64
	ScaleUpThreshold   float64
	ScaleDownThreshold float64
	Cooldown           time.Duration
}

/*
evaluate decides whether to scale up or down based on queue load.
*/
func (s *Scaler) evaluate() {
	s.pool.metrics.mu.Lock()

	if time.Since(s.lastScale) < s.cooldown {
		s.pool.metrics.mu.Unlock()
		return
	}

	workerCount := s.pool.metrics.WorkerCount
	queueSize := s.pool.metrics.JobQueueSize
	s.pool.metrics.mu.Unlock()

	if workerCount == 0 {
		if queueSize > 0 && s.maxWorkers > 0 {
			s.scaleUp(1)
			s.lastScale = time.Now()
		}

		return
	}

	currentLoad := float64(queueSize) / float64(workerCount)

	switch {
	case currentLoad > s.scaleUpThreshold && workerCount < s.maxWorkers:
		needed := int(math.Ceil(float64(queueSize) / s.targetLoad))
		toAdd := min(s.maxWorkers-workerCount, needed)

		if toAdd > 0 {
			s.scaleUp(toAdd)
			s.lastScale = time.Now()
		}

	case currentLoad < s.scaleDownThreshold && workerCount > s.minWorkers:
		needed := max(int(math.Ceil(float64(queueSize)/s.targetLoad)), s.minWorkers)
		toRemove := min(workerCount-s.minWorkers, max(1, (workerCount-needed)/2))

		if toRemove > 0 {
			s.scaleDown(toRemove)
			s.lastScale = time.Now()
		}
	}
}

/*
scaleUp adds the given number of workers.
*/
func (s *Scaler) scaleUp(count int) {
	s.pool.metrics.mu.Lock()
	workerCount := s.pool.metrics.WorkerCount
	s.pool.metrics.mu.Unlock()

	toAdd := min(s.maxWorkers-workerCount, max(1, count))

	for i := 0; i < toAdd; i++ {
		s.pool.startWorker()
	}

	log.Info("scaled up", "added", toAdd, "total", workerCount+toAdd)
}

/*
ScaleUpIfNeeded adds workers when under capacity.
Called from manage() when a job arrives (wc==0) or when waiting times out (all workers busy).
*/
func (s *Scaler) ScaleUpIfNeeded(count int) {
	s.pool.metrics.mu.Lock()
	wc := s.pool.metrics.WorkerCount
	s.pool.metrics.mu.Unlock()
	if wc < s.maxWorkers && count > 0 {
		toAdd := min(s.maxWorkers-wc, count)
		for i := 0; i < toAdd; i++ {
			s.pool.startWorker()
		}
	}
}

/*
scaleDown removes the given number of workers.
*/
func (s *Scaler) scaleDown(count int) {
	var toCancel []func()

	s.pool.workerMu.Lock()
	for range count {
		if len(s.pool.workerList) == 0 {
			break
		}
		lastIdx := len(s.pool.workerList) - 1
		w := s.pool.workerList[lastIdx]
		s.pool.workerList = s.pool.workerList[:lastIdx]
		s.pool.metrics.mu.Lock()
		s.pool.metrics.WorkerCount--
		s.pool.metrics.mu.Unlock()
		if w.cancel != nil {
			toCancel = append(toCancel, w.cancel)
		}
	}
	s.pool.workerMu.Unlock()

	for _, cancelFunc := range toCancel {
		cancelFunc()
		time.Sleep(50 * time.Millisecond)
	}
}

/*
run periodically evaluates and adjusts the worker count.
*/
func (s *Scaler) run() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.pool.ctx.Done():
			return
		case <-ticker.C:
			s.evaluate()
		}
	}
}

/*
NewScaler creates and starts a scaler for the given pool.
*/
func NewScaler(p *Pool, minWorkers, maxWorkers int, config *ScalerConfig) *Scaler {
	scaler := &Scaler{
		pool:               p,
		minWorkers:         minWorkers,
		maxWorkers:         maxWorkers,
		targetLoad:         config.TargetLoad,
		scaleUpThreshold:   config.ScaleUpThreshold,
		scaleDownThreshold: config.ScaleDownThreshold,
		cooldown:           config.Cooldown,
		lastScale:          time.Now(),
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		scaler.run()
	}()

	return scaler
}
