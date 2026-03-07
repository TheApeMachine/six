package pool

import (
	"context"
	"sync"
)

/*
Pool is a set of Worker types, each running their own pre-warmed goroutine.
Any object can schedule work on the worker pool. The pool auto-scales
to keep the system saturated without overloading it.
*/
type Pool struct {
	ctx           context.Context
	cancel        context.CancelFunc
	workers       chan chan Job
	jobs          chan Job
	activeWorkers []*Worker
	mu            sync.RWMutex
}

/*
NewPool instantiates a worker pool with an auto-scaler.
*/
func NewPool() *Pool {
	ctx, cancel := context.WithCancel(context.Background())

	p := &Pool{
		ctx:     ctx,
		cancel:  cancel,
		workers: make(chan chan Job, 10000), // generous buffer to prevent blocking
		jobs:    make(chan Job, 10000),
	}

	p.Run()

	return p
}

/*
Do schedules a new job onto the worker pool.
*/
func (p *Pool) Do(job Job) {
	p.jobs <- job
}

/*
Run starts the dispatcher and auto-scaler.
*/
func (p *Pool) Run() {
	NewScaler(p)
	go p.dispatch()
}

func (p *Pool) dispatch() {
	for {
		select {
		case job := <-p.jobs:
			// Get the first available worker
			jobChannel := <-p.workers
			// Send the job to the worker
			jobChannel <- job
		case <-p.ctx.Done():
			return
		}
	}
}

func (p *Pool) Close() {
	p.cancel()
}

func (p *Pool) addWorker() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.activeWorkers = append(p.activeWorkers, NewWorker(p))
}

func (p *Pool) removeWorker() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.activeWorkers) > 0 {
		worker := p.activeWorkers[0]
		p.activeWorkers = p.activeWorkers[1:]

		worker.Close()
	}
}

func (p *Pool) WorkerCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return len(p.activeWorkers)
}

func (p *Pool) TotalLatency() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var total int64

	for _, w := range p.activeWorkers {
		total += int64(w.Latency())
	}

	return total
}
