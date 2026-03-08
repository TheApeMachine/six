package pool

import (
	"math"
	"time"
)

// RetryPolicy defines how a job should be retried on failure.
type RetryPolicy struct {
	MaxAttempts int
	Strategy    RetryStrategy
	BackoffFunc func(attempt int) time.Duration
	Filter      func(error) bool
}

// RetryStrategy computes the delay before the next retry attempt.
type RetryStrategy interface {
	NextDelay(attempt int) time.Duration
}

// ExponentialBackoff doubles the delay on each attempt.
type ExponentialBackoff struct {
	Initial time.Duration
}

func (eb *ExponentialBackoff) NextDelay(attempt int) time.Duration {
	return eb.Initial * time.Duration(math.Pow(2, float64(attempt-1)))
}

// WithCircuitBreaker attaches circuit-breaker configuration to a job.
func WithCircuitBreaker(id string, maxFailures int, resetTimeout time.Duration) JobOption {
	return func(j *Job) {
		j.CircuitID = id
		j.CircuitConfig = &CircuitBreakerConfig{
			MaxFailures:  maxFailures,
			ResetTimeout: resetTimeout,
			HalfOpenMax:  2,
		}
	}
}

// WithRetry attaches a retry policy to a job.
func WithRetry(attempts int, strategy RetryStrategy) JobOption {
	return func(j *Job) {
		j.RetryPolicy = &RetryPolicy{
			MaxAttempts: attempts,
			Strategy:    strategy,
		}
	}
}
