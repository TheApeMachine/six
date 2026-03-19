package pool

import (
	"context"
	"time"
)

/*
Job represents a unit of work to be executed by the pool.
Fn implementations must respect ctx.Done() for cancellation.
*/
type Job struct {
	ID                    string
	ResultID              string
	Fn                    func(ctx context.Context) (any, error)
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

/*
CircuitBreakerConfig defines parameters for a circuit breaker.
*/
type CircuitBreakerConfig struct {
	MaxFailures  int
	ResetTimeout time.Duration
	HalfOpenMax  int
}

/*
WithDependencyRetry configures retry behaviour for dependency checks.
*/
func WithDependencyRetry(
	attempts int, strategy RetryStrategy,
) JobOption {
	return func(j *Job) {
		j.DependencyRetryPolicy = &RetryPolicy{
			MaxAttempts: attempts,
			Strategy:    strategy,
		}
	}
}

/*
WithDependencies sets the IDs of jobs this job depends on.
*/
func WithDependencies(dependencies []string) JobOption {
	return func(j *Job) {
		j.Dependencies = dependencies
	}
}

/*
WithTTL sets the time-to-live for the job's result.
*/
func WithTTL(ttl time.Duration) JobOption {
	return func(j *Job) {
		j.TTL = ttl
	}
}

/*
WithContext sets a custom context for the job.
*/
func WithContext(ctx context.Context) JobOption {
	return func(j *Job) {
		j.Ctx = ctx
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
	return func(j *Job) {
		j.CircuitID = id
		j.CircuitConfig = &CircuitBreakerConfig{
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
	return func(j *Job) {
		if j.CircuitConfig == nil {
			j.CircuitConfig = &CircuitBreakerConfig{HalfOpenMax: max}
			return
		}
		j.CircuitConfig.HalfOpenMax = max
	}
}

/*
WithRetry attaches a retry policy to the job.
*/
func WithRetry(attempts int, strategy RetryStrategy) JobOption {
	return func(j *Job) {
		j.RetryPolicy = &RetryPolicy{
			MaxAttempts: attempts,
			Strategy:    strategy,
		}
	}
}

/*
WithOnDrop registers a callback invoked when a job cannot be scheduled.
*/
func WithOnDrop(callback func(error)) JobOption {
	return func(j *Job) {
		j.OnDrop = callback
	}
}
