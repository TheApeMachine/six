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

