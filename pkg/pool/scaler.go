package pool

import (
	"math"
	"time"

	"github.com/charmbracelet/log"
)

// Scaler dynamically adjusts the pool's worker count based on queue load.
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

// ScalerConfig defines thresholds and timing for auto-scaling.
type ScalerConfig struct {
	TargetLoad         float64
	ScaleUpThreshold   float64
	ScaleDownThreshold float64
	Cooldown           time.Duration
}

func (s *Scaler) evaluate() {
	s.pool.metrics.mu.Lock()
	defer s.pool.metrics.mu.Unlock()

	if time.Since(s.lastScale) < s.cooldown {
		return
	}

	// With minWorkers=0, having 0 workers is valid. Scale up when jobs are queued.
	if s.pool.metrics.WorkerCount == 0 {
		if s.pool.metrics.JobQueueSize > 0 && s.maxWorkers > 0 {
			s.scaleUp(1)
			s.lastScale = time.Now()
		}
		return
	}

	currentLoad := float64(s.pool.metrics.JobQueueSize) / float64(s.pool.metrics.WorkerCount)

	switch {
	case currentLoad > s.scaleUpThreshold && s.pool.metrics.WorkerCount < s.maxWorkers:
		needed := int(math.Ceil(float64(s.pool.metrics.JobQueueSize) / s.targetLoad))
		toAdd := min(s.maxWorkers-s.pool.metrics.WorkerCount, needed)
		if toAdd > 0 {
			s.scaleUp(toAdd)
			s.lastScale = time.Now()
		}

	case currentLoad < s.scaleDownThreshold && s.pool.metrics.WorkerCount > s.minWorkers:
		needed := max(int(math.Ceil(float64(s.pool.metrics.JobQueueSize)/s.targetLoad)), s.minWorkers)
		toRemove := min(s.pool.metrics.WorkerCount-s.minWorkers, max(1, (s.pool.metrics.WorkerCount-needed)/2))
		if toRemove > 0 {
			s.scaleDown(toRemove)
			s.lastScale = time.Now()
		}
	}
}

func (s *Scaler) scaleUp(count int) {
	toAdd := min(s.maxWorkers-s.pool.metrics.WorkerCount, max(1, count))
	for i := 0; i < toAdd; i++ {
		s.pool.startWorker()
	}
	log.Info("scaled up", "added", toAdd, "total", s.pool.metrics.WorkerCount)
}

// ScaleUpIfNeeded adds workers when under capacity. Called from manage() when
// a job arrives (wc==0) or when waiting times out (all workers busy).
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

func (s *Scaler) scaleDown(count int) {
	s.pool.workerMu.Lock()
	defer s.pool.workerMu.Unlock()

	for range count {
		if len(s.pool.workerList) == 0 {
			break
		}

		w := s.pool.workerList[len(s.pool.workerList)-1]
		s.pool.workerList = s.pool.workerList[:len(s.pool.workerList)-1]
		cancelFunc := w.cancel
		s.pool.metrics.WorkerCount--

		s.pool.workerMu.Unlock()

		if cancelFunc != nil {
			cancelFunc()
		}
		time.Sleep(50 * time.Millisecond)

		s.pool.workerMu.Lock()
	}
}

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

// NewScaler creates and starts a scaler for the given pool.
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

	p.wg.Go(func() {
		scaler.run()
	})

	return scaler
}
