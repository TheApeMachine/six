package pool

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"
)

var defaultDependencyRetryStrategy RetryStrategy = &ExponentialBackoff{
	Initial: time.Second,
}

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
			closeErr := job.Close()

			if err != nil {
				w.pool.metrics.RecordJobFailure()
				w.recordFailure(job.CircuitID)
				if closeErr != nil {
					err = fmt.Errorf("%w: %w", err, closeErr)
				}

				w.pool.store.StoreError(job.ID, err, job.TTL)
			} else {
				if closeErr != nil {
					result = nil
					err = closeErr
					w.pool.metrics.RecordJobFailure()
					w.recordFailure(job.CircuitID)
					w.pool.store.StoreError(job.ID, err, job.TTL)
					continue
				}

				if writeErr := w.writeResultToTask(job, result); writeErr != nil {
					w.pool.metrics.RecordJobFailure()
					w.recordFailure(job.CircuitID)
					w.pool.store.StoreError(job.ID, writeErr, job.TTL)
					continue
				}

				w.pool.metrics.RecordJobSuccess(time.Since(job.StartTime))
				w.recordSuccess(job.CircuitID)
				w.pool.store.Store(job.ID, result, job.TTL)
			}
		}
	}
}

/*
processJobWithTimeout runs the job Task with timeout and retry handling.
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
runSingleAttempt executes a single attempt by draining task output.
*/
func (w *Worker) runSingleAttempt(ctx context.Context, job Job) (any, error) {
	if job.Task == nil {
		return nil, fmt.Errorf("job %s task is nil", job.ID)
	}

	type singleAttemptResult struct {
		value any
		err   error
	}

	resultChan := make(chan singleAttemptResult, 1)

	go func() {
		var buffer bytes.Buffer
		readBytes, copyErr := io.Copy(&buffer, job.Task)
		if copyErr == io.EOF {
			copyErr = nil
		}

		var result any
		if buffer.Len() > 0 {
			result = buffer.Bytes()
		} else {
			result = readBytes
		}

		resultChan <- singleAttemptResult{
			value: result,
			err:   copyErr,
		}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("job %s timed out", job.ID)
	case result := <-resultChan:
		return result.value, result.err
	}
}

/*
checkSingleDependency waits for depID to be available, retrying according to policy.
*/
func (w *Worker) checkSingleDependency(depID string, retryPolicy *RetryPolicy) error {
	maxAttempts := 1
	strategy := defaultDependencyRetryStrategy

	if retryPolicy != nil {
		maxAttempts = retryPolicy.MaxAttempts
		if retryPolicy.Strategy != nil {
			strategy = retryPolicy.Strategy
		}
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

		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			result, ok := w.pool.store.Result(depID)
			if ok {
				if result.Error == nil {
					return nil
				}
				break
			}
			time.Sleep(10 * time.Millisecond)
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
writeResultToTask writes task output bytes back into the task shell.
*/
func (w *Worker) writeResultToTask(job Job, value any) error {
	bytesValue, ok := value.([]byte)
	if !ok || len(bytesValue) == 0 {
		return nil
	}

	_, err := job.Write(bytesValue)
	return err
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
