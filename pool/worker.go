package pool

import (
	"sync/atomic"
	"time"
)

/*
Worker wraps a concurrent process that processes Jobs scheduled onto a Pool.
It registers itself with the Pool when it's ready for new work.
*/
type Worker struct {
	pool    *Pool
	jobs    chan Job
	quit    chan struct{}
	latency atomic.Int64 // latency in nanoseconds, atomic for thread-safe reads by the scaler
}

func NewWorker(p *Pool) *Worker {
	w := &Worker{
		pool: p,
		jobs: make(chan Job),
		quit: make(chan struct{}),
	}
	w.start()
	return w
}

func (w *Worker) start() {
	go func() {
		for {
			// Register this worker's channel to the pool's available workers queue
			w.pool.workers <- w.jobs

			select {
			case job := <-w.jobs:
				t := time.Now()
				job()
				w.latency.Store(time.Since(t).Nanoseconds())
			case <-w.quit:
				return
			case <-w.pool.ctx.Done():
				return
			}
		}
	}()
}

func (w *Worker) Close() {
	close(w.quit)
}

func (w *Worker) Latency() time.Duration {
	return time.Duration(w.latency.Load())
}
