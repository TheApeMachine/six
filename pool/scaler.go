package pool

import (
	"time"

	config "github.com/theapemachine/six/core"
)

/*
Scaler evaluates the latency of workers in a Pool and scales the
number of workers up or down to division load optimally.
*/
type Scaler struct {
	pool       *Pool
	current    *state
	minWorkers int
	maxWorkers int
}

type state struct {
	prev  int64
	ok    bool
	trend bool
}

func NewScaler(p *Pool) *Scaler {
	s := &Scaler{
		pool:       p,
		current:    &state{prev: 0, ok: true, trend: false},
		minWorkers: config.Workers.Min,
		maxWorkers: config.Workers.Max,
	}

	s.initialize()
	return s
}

func (scaler *Scaler) initialize() {
	go func() {
		var latency int64

		for {
			scaler.current.prev = latency
			latency = scaler.pool.TotalLatency()

			// If no latency and no workers, bootstrap one to get measurements
			if latency == 0 && scaler.pool.WorkerCount() == 0 {
				scaler.pool.addWorker()
				// Send a no-op job to trigger latency measurement
				scaler.pool.Do(func() {})
				continue
			}

			// Don't overload scaling decisions
			time.Sleep(100 * time.Millisecond)

			// Fast-path protection: if context done, exit
			select {
			case <-scaler.pool.ctx.Done():
				return
			default:
			}

			// Observe latency trends
			if latency > scaler.current.prev && scaler.current.ok {
				// Latency rising, load increasing or backing up
				scaler.current.ok = false
				scaler.current.trend = false
				continue
			}

			if latency < scaler.current.prev && !scaler.current.ok {
				// Latency dropping, recovering
				scaler.current.ok = true
				scaler.current.trend = false
				continue
			}

			if !scaler.current.trend {
				// We need two ticks to establish a trend
				scaler.current.trend = true
				continue
			}

			// Max cap on workers to prevent exploding goroutines
			workerCount := scaler.pool.WorkerCount()

			switch scaler.current.ok {
			case true:
				// Latency is stable or dropping, safe to scale up for more throughput
				// if we have queued jobs. We cap it to avoid extreme scaling.
				if workerCount < 1024 {
					scaler.pool.addWorker()
				}
			case false:
				// Latency is rising, scale down to reduce context switching
				if workerCount > 1 {
					scaler.pool.removeWorker()
				}
			}

			scaler.current = &state{prev: latency, ok: true, trend: false}
		}
	}()
}

type ScalerError string

const (
	ErrBadMaxWorkerConfig ScalerError = "max workers config is bad"
)

func (scaler ScalerError) Error() string {
	return string(scaler)
}
