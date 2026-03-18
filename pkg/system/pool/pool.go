package pool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
)

/*
Pool is a dynamically-scaling worker pool with circuit breakers,
backpressure, metrics, and broadcast groups.
*/
type Pool struct {
	ctx        context.Context
	cancel     context.CancelFunc
	quit       chan struct{}
	wg         sync.WaitGroup
	workers    chan chan Job
	jobs       chan Job
	store      *ResultStore
	scaler     *Scaler
	metrics    *Metrics
	breakers   map[string]*CircuitBreaker
	workerMu   sync.Mutex
	workerList []*Worker
	breakersMu sync.RWMutex
	jobSeq     atomic.Uint64
	config     *Config
}

/*
New creates a Pool that starts with minWorkers goroutines and scales
up to maxWorkers based on queue depth.
*/
func New(ctx context.Context, minWorkers, maxWorkers int, config *Config) *Pool {
	ctx, cancel := context.WithCancel(ctx)
	p := &Pool{
		ctx:        ctx,
		cancel:     cancel,
		breakers:   make(map[string]*CircuitBreaker),
		workerList: make([]*Worker, 0),
		quit:       make(chan struct{}),
		jobs:       make(chan Job, maxWorkers*10),
		workers:    make(chan chan Job, maxWorkers),
		store:      NewResultStore(),
		metrics:    NewMetrics(),
		config:     config,
	}

	for i := 0; i < minWorkers; i++ {
		p.startWorker()
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.manage()
	}()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.collectMetrics()
	}()

	scalerConfig := &ScalerConfig{
		TargetLoad:         2.0,
		ScaleUpThreshold:   4.0,
		ScaleDownThreshold: 1.0,
		Cooldown:           500 * time.Millisecond,
	}
	p.scaler = NewScaler(p, minWorkers, maxWorkers, scalerConfig)

	return p
}

/*
manage dispatches jobs from the queue to available workers.
*/
func (p *Pool) manage() {
	retryTimer := time.NewTimer(100 * time.Millisecond)
	if !retryTimer.Stop() {
		<-retryTimer.C
	}

	defer retryTimer.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case job, ok := <-p.jobs:
			if !ok {
				return
			}

			// Scale up on demand when no workers or all busy.
			if p.scaler != nil {
				p.scaler.ScaleUpIfNeeded(1)
			}

			deadline := time.Now().Add(p.getSchedulingTimeout())
			dispatched := false
			for !dispatched && time.Now().Before(deadline) {
				if !retryTimer.Stop() {
					select {
					case <-retryTimer.C:
					default:
					}
				}
				retryTimer.Reset(100 * time.Millisecond)

				select {
				case <-p.ctx.Done():
					return
				case workerChan := <-p.workers:
					select {
					case workerChan <- job:
						dispatched = true
					case <-p.ctx.Done():
						return
					}
				case <-retryTimer.C:
					if p.scaler != nil {
						p.scaler.ScaleUpIfNeeded(1)
					}
				}
			}
			if !dispatched {
				log.Warn("no available workers, job timed out", "job", job.ID)
				p.store.StoreError(job.ID, fmt.Errorf("no available workers"), job.TTL)
			}
		}
	}
}

/*
collectMetrics periodically updates queue size and idle worker count.
*/
func (p *Pool) collectMetrics() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.metrics.mu.Lock()
			p.metrics.JobQueueSize = len(p.jobs)
			p.metrics.IdleWorkers = len(p.workers)
			p.metrics.mu.Unlock()
		}
	}
}

/*
Schedule submits a job and returns a channel that will receive the result.
*/
func (p *Pool) Schedule(id string, fn func(ctx context.Context) (any, error), opts ...JobOption) chan *Result {
	ctx, cancel := context.WithTimeout(p.ctx, p.getSchedulingTimeout())
	defer cancel()

	job := Job{
		ID: id,
		Fn: fn,
		RetryPolicy: &RetryPolicy{
			MaxAttempts: 3,
			Strategy:    &ExponentialBackoff{Initial: time.Second},
		},
		StartTime: time.Now(),
	}

	for _, opt := range opts {
		opt(&job)
	}

	if job.CircuitID != "" {
		breaker := p.getCircuitBreaker(job)
		if breaker != nil && !breaker.Allow() {
			ch := make(chan *Result, 1)
			ch <- &Result{
				Error:     fmt.Errorf("circuit breaker %s is open", job.CircuitID),
				CreatedAt: time.Now(),
			}
			close(ch)
			return ch
		}
	}

	job.ResultID = fmt.Sprintf("%s/%d", job.ID, p.jobSeq.Add(1))
	resultCh := p.store.prepare(job.ResultID)

	select {
	case p.jobs <- job:
		return resultCh
	case <-ctx.Done():
		p.store.cancelAwait(job.ResultID, resultCh)

		ch := make(chan *Result, 1)
		ch <- &Result{
			Error:     fmt.Errorf("job scheduling timeout: %w", ctx.Err()),
			CreatedAt: time.Now(),
		}
		close(ch)

		p.metrics.mu.Lock()
		p.metrics.SchedulingFailures++
		p.metrics.mu.Unlock()

		return ch
	}
}

/*
CreateBroadcastGroup creates a named broadcast group for fan-out.
*/
func (p *Pool) CreateBroadcastGroup(id string, ttl time.Duration) *BroadcastGroup {
	return p.store.CreateBroadcastGroup(id, ttl)
}

/*
Subscribe returns a channel receiving results from a broadcast group.
*/
func (p *Pool) Subscribe(groupID string) chan *Result {
	return p.store.Subscribe(groupID)
}

/*
Metrics returns the pool's metrics instance.
*/
func (p *Pool) Metrics() *Metrics {
	return p.metrics
}

/*
startWorker spawns a new worker goroutine and registers it.
*/
func (p *Pool) startWorker() {
	worker := &Worker{
		pool:   p,
		jobs:   make(chan Job),
		cancel: nil,
	}
	p.workerMu.Lock()
	p.workerList = append(p.workerList, worker)
	p.workerMu.Unlock()

	p.metrics.mu.Lock()
	p.metrics.WorkerCount++
	p.metrics.mu.Unlock()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		worker.run()
	}()
}

/*
getCircuitBreaker returns or creates the circuit breaker for the job's circuit ID.
*/
func (p *Pool) getCircuitBreaker(job Job) *CircuitBreaker {
	if job.CircuitID == "" || job.CircuitConfig == nil {
		return nil
	}

	p.breakersMu.Lock()
	defer p.breakersMu.Unlock()

	breaker, exists := p.breakers[job.CircuitID]
	if !exists {
		breaker = &CircuitBreaker{
			maxFailures:  job.CircuitConfig.MaxFailures,
			resetTimeout: job.CircuitConfig.ResetTimeout,
			halfOpenMax:  job.CircuitConfig.HalfOpenMax,
			state:        CircuitClosed,
		}
		p.breakers[job.CircuitID] = breaker
	}

	return breaker
}

/*
getSchedulingTimeout returns the configured or default scheduling timeout.
*/
func (p *Pool) getSchedulingTimeout() time.Duration {
	if p.config != nil && p.config.SchedulingTimeout > 0 {
		return p.config.SchedulingTimeout
	}
	return 5 * time.Second
}

/*
Close gracefully shuts down the pool, draining in-flight jobs.
*/
func (p *Pool) Close() {
	if p == nil {
		return
	}

	p.cancel()
	p.wg.Wait()

	p.workerMu.Lock()
	for _, worker := range p.workerList {
		close(worker.jobs)
	}
	p.workerList = nil
	p.workerMu.Unlock()

	close(p.quit)
	close(p.jobs)
	close(p.workers)

	p.store.Close()
}
