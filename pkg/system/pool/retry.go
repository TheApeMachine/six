package pool

import (
	"math"
	"math/rand"
	"time"
)

/*
RetryPolicy defines how a job should be retried on failure.
*/
type RetryPolicy struct {
	MaxAttempts           int
	Strategy              RetryStrategy
	Filter                func(error) bool
	DependencyAwaitTimeout time.Duration
}

/*
RetryStrategy computes the delay before the next retry attempt.
*/
type RetryStrategy interface {
	NextDelay(attempt int) time.Duration
}

/*
ExponentialBackoff doubles the delay on each attempt with optional cap and jitter.
*/
type ExponentialBackoff struct {
	Initial  time.Duration
	MaxDelay time.Duration // 0 means no cap
	Jitter   float64      // 0.0-1.0; 0 means no jitter
}

/*
NextDelay returns the delay before the next retry attempt.
*/
func (eb *ExponentialBackoff) NextDelay(attempt int) time.Duration {
	base := eb.Initial * time.Duration(math.Pow(2, float64(attempt-1)))
	if eb.MaxDelay > 0 && base > eb.MaxDelay {
		base = eb.MaxDelay
	}
	if eb.Jitter <= 0 {
		return base
	}
	jitterFactor := 1.0 + (eb.Jitter*2)*(rand.Float64()-0.5)
	if jitterFactor < 0 {
		jitterFactor = 0
	}
	return time.Duration(float64(base) * jitterFactor)
}

