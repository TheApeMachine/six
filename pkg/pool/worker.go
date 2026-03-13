package pool

import (
	"context"
	"fmt"
	"time"
)

// Worker processes jobs received from the pool.
type Worker struct {
	pool       *Pool
	jobs       chan Job
	cancel     context.CancelFunc
	currentJob *Job
}

func (w *Worker) run() {
	jobChan := w.jobs

	for {
		select {
		case <-w.pool.ctx.Done():
			return
		default:
		}

		w.pool.workers <- jobChan

		select {
		case <-w.pool.ctx.Done():
			return
		case job, ok := <-jobChan:
			if !ok {
				return
			}

			w.currentJob = &job

			runCtx := w.pool.ctx
			if job.Ctx != nil {
				runCtx = job.Ctx
			}

			result, err := w.processJobWithTimeout(runCtx, job)
			w.currentJob = nil

			if err != nil {
				w.pool.metrics.RecordJobFailure()
				w.pool.store.deliver(job.ResultID, &Result{
					Error:     err,
					TTL:       job.TTL,
					CreatedAt: time.Now(),
				})
				w.pool.store.StoreError(job.ID, err, job.TTL)
			} else {
				w.pool.metrics.RecordJobSuccess(time.Since(job.StartTime))
				w.pool.store.deliver(job.ResultID, &Result{
					Value:     result,
					TTL:       job.TTL,
					CreatedAt: time.Now(),
				})
				w.pool.store.Store(job.ID, result, job.TTL)
			}
		}
	}
}

func (w *Worker) processJobWithTimeout(ctx context.Context, job Job) (any, error) {
	startTime := time.Now()

	for _, depID := range job.Dependencies {
		if err := w.checkSingleDependency(depID, job.DependencyRetryPolicy); err != nil {
			w.pool.metrics.RecordJobExecution(startTime, false)
			if job.CircuitID != "" {
				w.recordFailure(job.CircuitID)
			}
			return nil, err
		}
	}

	done := make(chan struct{})
	var result any
	var err error

	go func() {
		defer close(done)
		result, err = job.Fn()
	}()

	select {
	case <-ctx.Done():
		w.pool.metrics.RecordJobFailure()
		return nil, fmt.Errorf("job %s timed out", job.ID)
	case <-done:
		w.pool.metrics.RecordJobExecution(startTime, err == nil)
		return result, err
	}
}

func (w *Worker) checkSingleDependency(depID string, retryPolicy *RetryPolicy) error {
	maxAttempts := 1
	var strategy RetryStrategy = &ExponentialBackoff{Initial: time.Second}

	if retryPolicy != nil {
		maxAttempts = retryPolicy.MaxAttempts
		strategy = retryPolicy.Strategy
	}

	circuitID := ""
	if w.currentJob != nil {
		circuitID = w.currentJob.CircuitID
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if !w.pool.store.Exists(depID) {
			if attempt < maxAttempts-1 {
				time.Sleep(strategy.NextDelay(attempt + 1))
				continue
			}
			break
		}

		ch := w.pool.store.Await(depID)
		select {
		case result := <-ch:
			if result.Error == nil {
				return nil
			}
		case <-time.After(time.Second):
		}

		if attempt < maxAttempts-1 {
			time.Sleep(strategy.NextDelay(attempt + 1))
			continue
		}
	}

	w.pool.breakersMu.RLock()
	breaker, exists := w.pool.breakers[circuitID]
	w.pool.breakersMu.RUnlock()

	if exists {
		breaker.RecordFailure()
	}

	w.pool.store.mu.Lock()
	if w.pool.store.children == nil {
		w.pool.store.children = make(map[string][]string)
	}
	if w.currentJob != nil {
		w.pool.store.children[depID] = append(w.pool.store.children[depID], w.currentJob.ID)
	}
	w.pool.store.mu.Unlock()

	return fmt.Errorf("dependency %s failed after %d attempts", depID, maxAttempts)
}

func (w *Worker) recordFailure(circuitID string) {
	if circuitID == "" {
		return
	}

	w.pool.breakersMu.RLock()
	breaker, exists := w.pool.breakers[circuitID]
	w.pool.breakersMu.RUnlock()

	if exists {
		breaker.RecordFailure()
	}
}
