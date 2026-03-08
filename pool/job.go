package pool

import "time"

// Job represents a unit of work to be executed by the pool.
type Job struct {
	ID                    string
	Fn                    func() (any, error)
	RetryPolicy           *RetryPolicy
	CircuitID             string
	CircuitConfig         *CircuitBreakerConfig
	Dependencies          []string
	TTL                   time.Duration
	Attempt               int
	LastError             error
	DependencyRetryPolicy *RetryPolicy
	StartTime             time.Time
}

// JobOption configures a Job before submission.
type JobOption func(*Job)

// CircuitBreakerConfig defines parameters for a circuit breaker.
type CircuitBreakerConfig struct {
	MaxFailures  int
	ResetTimeout time.Duration
	HalfOpenMax  int
}

// WithDependencyRetry configures retry behaviour for dependency checks.
func WithDependencyRetry(attempts int, strategy RetryStrategy) JobOption {
	return func(j *Job) {
		j.DependencyRetryPolicy = &RetryPolicy{
			MaxAttempts: attempts,
			Strategy:    strategy,
		}
	}
}

// WithDependencies sets the IDs of jobs this job depends on.
func WithDependencies(dependencies []string) JobOption {
	return func(j *Job) {
		j.Dependencies = dependencies
	}
}

// WithTTL sets the time-to-live for the job's result.
func WithTTL(ttl time.Duration) JobOption {
	return func(j *Job) {
		j.TTL = ttl
	}
}
