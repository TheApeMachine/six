package pool

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
)

var defaultScheduleRetryStrategy RetryStrategy = &ExponentialBackoff{
	Initial: time.Second,
}

/*
Pool is a dynamically-scaling worker pool with circuit breakers,
backpressure, metrics, and broadcast groups.
*/
type Pool struct {
	ctx        context.Context
	cancel     context.CancelFunc
	quit       chan struct{}
	wg         sync.WaitGroup
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
	nextWorker atomic.Uint64
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
manage dispatches jobs to workers via round-robin index, eliminating the
double-channel handshake of chan-chan. Each worker has a buffered(1) job
channel so the dispatcher does not block on a single busy worker.
*/
func (p *Pool) manage() {
	for {
		select {
		case <-p.ctx.Done():
			return
		case job, ok := <-p.jobs:
			if !ok {
				return
			}

			if p.scaler != nil {
				p.scaler.ScaleUpIfNeeded(1)
			}

			dispatched := false
			deadline := time.Now().Add(p.getSchedulingTimeout())

			for !dispatched && time.Now().Before(deadline) {
				p.workerMu.Lock()
				workerCount := len(p.workerList)
				p.workerMu.Unlock()

				if workerCount == 0 {
					if p.scaler != nil {
						p.scaler.ScaleUpIfNeeded(1)
					}

					time.Sleep(time.Millisecond)
					continue
				}

				idx := p.nextWorker.Add(1) % uint64(workerCount)

				p.workerMu.Lock()
				if int(idx) >= len(p.workerList) {
					p.workerMu.Unlock()
					continue
				}
				worker := p.workerList[idx]
				p.workerMu.Unlock()

				select {
				case worker.jobs <- job:
					dispatched = true
				case <-time.After(10 * time.Millisecond):
					continue
				case <-p.ctx.Done():
					return
				}
			}

			if !dispatched {
				log.Warn("no available workers, job timed out", "job", job.ID)
				err := fmt.Errorf("no available workers")

				if job.OnDrop != nil {
					job.OnDrop(err)
				}

				p.store.StoreError(job.ID, err, job.TTL)
			}
		}
	}
}

/*
collectMetrics periodically updates queue size and idle worker count.
Idle is approximated by counting workers whose job channel has room.
*/
func (p *Pool) collectMetrics() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.workerMu.Lock()
			idle := 0
			for _, worker := range p.workerList {
				if len(worker.jobs) == 0 {
					idle++
				}
			}
			p.workerMu.Unlock()

			p.metrics.mu.Lock()
			p.metrics.JobQueueSize = len(p.jobs)
			p.metrics.IdleWorkers = idle
			p.metrics.mu.Unlock()
		}
	}
}

/*
Schedule submits a task-oriented job to the worker queue.
*/
func (p *Pool) Schedule(
	id string,
	taskType TaskType,
	task Task,
	opts ...JobOption,
) error {
	job := Job{
		ID:       id,
		TaskType: taskType,
		Task:     task,
		RetryPolicy: &RetryPolicy{
			MaxAttempts: 3,
			Strategy:    defaultScheduleRetryStrategy,
		},
		Ctx:       p.ctx,
		StartTime: time.Now(),
	}

	for _, opt := range opts {
		opt(&job)
	}

	if job.Task == nil {
		return fmt.Errorf("job task is nil")
	}

	if job.StartTime.IsZero() {
		job.StartTime = time.Now()
	}

	if job.RetryPolicy == nil {
		job.RetryPolicy = &RetryPolicy{
			MaxAttempts: 3,
			Strategy:    defaultScheduleRetryStrategy,
		}
	}

	if job.Ctx == nil {
		job.Ctx = p.ctx
	}

	if job.CircuitID != "" {
		breaker := p.getCircuitBreaker(job)
		if breaker != nil && !breaker.Allow() {
			err := fmt.Errorf("circuit breaker %s is open", job.CircuitID)
			if job.OnDrop != nil {
				job.OnDrop(err)
			}
			return err
		}
	}

	job.ResultID = p.nextResultID(job.ID)

	select {
	case <-p.ctx.Done():
		err := fmt.Errorf("job scheduling timeout: %w", p.ctx.Err())

		if job.OnDrop != nil {
			job.OnDrop(err)
		}

		p.store.StoreError(job.ID, err, job.TTL)

		p.metrics.mu.Lock()
		p.metrics.SchedulingFailures++
		p.metrics.mu.Unlock()
		return err
	default:
	}

	select {
	case p.jobs <- job:
		return nil
	default:
	}

	scheduleTimeout := p.getSchedulingTimeout()
	scheduleTimer := time.NewTimer(scheduleTimeout)
	defer scheduleTimer.Stop()

	select {
	case p.jobs <- job:
		return nil
	case <-scheduleTimer.C:
		err := fmt.Errorf("job scheduling timeout: %w", context.DeadlineExceeded)
		if job.OnDrop != nil {
			job.OnDrop(err)
		}

		p.store.StoreError(job.ID, err, job.TTL)

		p.metrics.mu.Lock()
		p.metrics.SchedulingFailures++
		p.metrics.mu.Unlock()
		return err
	case <-p.ctx.Done():
		err := fmt.Errorf("job scheduling timeout: %w", p.ctx.Err())

		if job.OnDrop != nil {
			job.OnDrop(err)
		}

		p.store.StoreError(job.ID, err, job.TTL)

		p.metrics.mu.Lock()
		p.metrics.SchedulingFailures++
		p.metrics.mu.Unlock()
		return err
	}
}

/*
nextResultID constructs a stable result key from job ID and sequence number.
*/
func (p *Pool) nextResultID(jobID string) string {
	sequence := p.jobSeq.Add(1)
	var sequenceBuffer [20]byte
	sequenceBytes := strconv.AppendUint(sequenceBuffer[:0], sequence, 10)

	var idBuilder strings.Builder
	idBuilder.Grow(len(jobID) + 1 + len(sequenceBytes))
	idBuilder.WriteString(jobID)
	idBuilder.WriteByte('/')
	idBuilder.Write(sequenceBytes)

	return idBuilder.String()
}

/*
Read forwards stream reads to the internal ResultStore shell.
*/
func (p *Pool) Read(buf []byte) (n int, err error) {
	return p.store.Read(buf)
}

/*
Write forwards stream writes to the internal ResultStore shell.
*/
func (p *Pool) Write(buf []byte) (n int, err error) {
	return p.store.Write(buf)
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
StoredResult returns a completed job result from the store.
*/
func (p *Pool) StoredResult(id string) (*Result, bool) {
	if p == nil || p.store == nil {
		return nil, false
	}

	return p.store.Result(id)
}

/*
Metrics returns the pool's metrics instance.
*/
func (p *Pool) Metrics() *Metrics {
	return p.metrics
}

/*
WorkerCount returns the number of running worker goroutines.
*/
func (p *Pool) WorkerCount() int {
	p.workerMu.Lock()
	defer p.workerMu.Unlock()

	return len(p.workerList)
}

/*
WaitForWorkerCount blocks until WorkerCount is at least minCount or ctx is done.
*/
func (p *Pool) WaitForWorkerCount(ctx context.Context, minCount int) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for p.WorkerCount() < minCount {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}

	return nil
}

/*
startWorker spawns a new worker goroutine and registers it.
*/
func (p *Pool) startWorker() {
	workerCtx, workerCancel := context.WithCancel(p.ctx)

	worker := &Worker{
		pool:   p,
		ctx:    workerCtx,
		jobs:   make(chan Job, 1),
		cancel: workerCancel,
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
func (p *Pool) Close() error {
	if p == nil {
		return nil
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

	return p.store.Close()
}
