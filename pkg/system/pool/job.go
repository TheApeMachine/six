package pool

import (
	"context"
	"fmt"
	"io"
	"time"
)

type Task interface {
	io.ReadWriteCloser
}

type TaskType uint

const (
	LOAD TaskType = iota
	STORE
	COMPUTE
	EVALUATE
)

/*
Job represents a unit of work to be executed by the pool.
Task implementations must respect ctx.Done() for cancellation.
*/
type Job struct {
	ID                    string
	ResultID              string
	Task                  Task
	TaskType              TaskType
	OnDrop                func(error)
	RetryPolicy           *RetryPolicy
	CircuitID             string
	CircuitConfig         *CircuitBreakerConfig
	Dependencies          []string
	TTL                   time.Duration
	Attempt               int
	LastError             error
	DependencyRetryPolicy *RetryPolicy
	StartTime             time.Time
	Ctx                   context.Context
}

/*
JobOption configures a Job before submission.
*/
type JobOption func(*Job)

func NewJob(opts ...JobOption) Job {
	job := Job{}

	for _, opt := range opts {
		opt(&job)
	}

	return job
}

func (job *Job) Read(p []byte) (n int, err error) {
	if job.Task == nil {
		return 0, fmt.Errorf("job task is nil")
	}

	return job.Task.Read(p)
}

func (job *Job) Write(p []byte) (n int, err error) {
	if job.Task == nil {
		return 0, fmt.Errorf("job task is nil")
	}

	return job.Task.Write(p)
}

func (job *Job) Close() error {
	if job.Task == nil {
		return nil
	}

	return job.Task.Close()
}

/*
CircuitBreakerConfig defines parameters for a circuit breaker.
*/
type CircuitBreakerConfig struct {
	MaxFailures  int
	ResetTimeout time.Duration
	HalfOpenMax  int
}

func WithID(id string) JobOption {
	return func(job *Job) {
		job.ID = id
	}
}

func WithResultID(resultID string) JobOption {
	return func(job *Job) {
		job.ResultID = resultID
	}
}

func WithStartTime(startTime time.Time) JobOption {
	return func(job *Job) {
		job.StartTime = startTime
	}
}

func WithTask(taskType TaskType, task Task) JobOption {
	return func(job *Job) {
		job.TaskType = taskType
		job.Task = task
	}
}

/*
WithDependencyRetry configures retry behaviour for dependency checks.
*/
func WithDependencyRetry(
	attempts int, strategy RetryStrategy,
) JobOption {
	return func(job *Job) {
		job.DependencyRetryPolicy = &RetryPolicy{
			MaxAttempts: attempts,
			Strategy:    strategy,
		}
	}
}

/*
WithDependencies sets the IDs of jobs this job depends on.
*/
func WithDependencies(dependencies []string) JobOption {
	return func(job *Job) {
		job.Dependencies = dependencies
	}
}

/*
WithTTL sets the time-to-live for the job's result.
*/
func WithTTL(ttl time.Duration) JobOption {
	return func(job *Job) {
		job.TTL = ttl
	}
}

/*
WithContext sets a custom context for the job.
*/
func WithContext(ctx context.Context) JobOption {
	return func(job *Job) {
		job.Ctx = ctx
	}
}

/*
WithCircuitBreaker attaches circuit-breaker configuration to a job.
*/
func WithCircuitBreaker(
	id string,
	maxFailures int,
	resetTimeout time.Duration,
	halfOpenMax int,
) JobOption {
	return func(job *Job) {
		job.CircuitID = id
		job.CircuitConfig = &CircuitBreakerConfig{
			MaxFailures:  maxFailures,
			ResetTimeout: resetTimeout,
			HalfOpenMax:  halfOpenMax,
		}
	}
}

/*
WithHalfOpenMax overrides HalfOpenMax when CircuitConfig is already set.
*/
func WithHalfOpenMax(max int) JobOption {
	return func(job *Job) {
		if job.CircuitConfig == nil {
			job.CircuitConfig = &CircuitBreakerConfig{HalfOpenMax: max}
			return
		}
		job.CircuitConfig.HalfOpenMax = max
	}
}

/*
WithRetry attaches a retry policy to the job.
*/
func WithRetry(attempts int, strategy RetryStrategy) JobOption {
	return func(job *Job) {
		job.RetryPolicy = &RetryPolicy{
			MaxAttempts: attempts,
			Strategy:    strategy,
		}
	}
}

/*
WithOnDrop registers a callback invoked when a job cannot be scheduled.
*/
func WithOnDrop(callback func(error)) JobOption {
	return func(job *Job) {
		job.OnDrop = callback
	}
}
