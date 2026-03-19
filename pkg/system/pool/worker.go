package pool

import (
	"context"
	"fmt"
	"time"
)

/*
Worker processes jobs received from the pool.
*/
type Worker struct {
	pool       *Pool
	ctx        context.Context
	jobs       chan Job
	cancel     context.CancelFunc
	currentJob *Job
}

/*
run loops receiving jobs and executing them.
*/
func (w *Worker) run() {
	jobChan := w.jobs

	for {
		select {
		case <-w.ctx.Done():
			return
		case w.pool.workers <- jobChan:
		}

		select {
		case <-w.ctx.Done():
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
				w.recordFailure(job.CircuitID)
				w.pool.store.deliver(job.ResultID, &Result{
					Error:     err,
					TTL:       job.TTL,
					CreatedAt: time.Now(),
				})
				w.pool.store.StoreError(job.ID, err, job.TTL)
			} else {
				w.pool.metrics.RecordJobSuccess(time.Since(job.StartTime))
				w.recordSuccess(job.CircuitID)
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

/*
processJobWithTimeout runs the job Fn with context cancellation and timeout handling.
*/
func (w *Worker) processJobWithTimeout(ctx context.Context, job Job) (any, error) {
	for _, depID := range job.Dependencies {
		if err := w.checkSingleDependency(depID, job.DependencyRetryPolicy); err != nil {
			return nil, err
		}
	}

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("job %s timed out", job.ID)
	default:
	}

	retryPolicy := job.RetryPolicy
	maxAttempts := 1
	var strategy RetryStrategy
	var filter func(error) bool

	if retryPolicy != nil {
		maxAttempts = max(retryPolicy.MaxAttempts, 1)
		strategy = retryPolicy.Strategy
		filter = retryPolicy.Filter
	}

	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := w.runSingleAttempt(ctx, job)
		if err == nil {
			return result, nil
		}

		lastErr = err

		if filter != nil && !filter(err) {
			break
		}

		if attempt == maxAttempts {
			break
		}

		delay := time.Duration(0)
		if strategy != nil {
			delay = strategy.NextDelay(attempt)
		}

		if delay <= 0 {
			continue
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("job %s timed out", job.ID)
		case <-time.After(delay):
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}

	return nil, fmt.Errorf("job %s failed without result", job.ID)
}

/*
runSingleAttempt executes a single attempt of job.Fn with context timeout handling.
*/
func (w *Worker) runSingleAttempt(ctx context.Context, job Job) (any, error) {
	resultChan := make(chan *Result, 1)

	go func() {
		result, err := job.Fn(ctx)
		resultChan <- &Result{
			Value: result,
			Error: err,
		}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("job %s timed out", job.ID)
	case result := <-resultChan:
		return result.Value, result.Error
	}
}

/*
checkSingleDependency waits for depID to be available, retrying according to policy.
*/
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

		timeout := time.Second
		if retryPolicy != nil && retryPolicy.DependencyAwaitTimeout > 0 {
			timeout = retryPolicy.DependencyAwaitTimeout
		} else if w.pool.config != nil && w.pool.config.DependencyAwaitTimeout > 0 {
			timeout = w.pool.config.DependencyAwaitTimeout
		}

		ch := w.pool.store.Await(depID)
		select {
		case result := <-ch:
			if result.Error == nil {
				return nil
			}
		case <-time.After(timeout):
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

	if w.currentJob != nil {
		w.pool.store.AddChildDependency(depID, w.currentJob.ID)
	}

	return fmt.Errorf("dependency %s failed after %d attempts", depID, maxAttempts)
}

/*
recordFailure notifies the circuit breaker for the given circuit ID.
*/
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

/*
recordSuccess notifies the circuit breaker about a successful job completion.
*/
func (w *Worker) recordSuccess(circuitID string) {
	if circuitID == "" {
		return
	}

	w.pool.breakersMu.RLock()
	breaker, exists := w.pool.breakers[circuitID]
	w.pool.breakersMu.RUnlock()

	if exists {
		breaker.RecordSuccess()
	}
}
