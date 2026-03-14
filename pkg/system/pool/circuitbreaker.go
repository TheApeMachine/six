package pool

import (
	"sync"
	"time"

	"github.com/charmbracelet/log"
)

/*
CircuitState represents the operational state of a circuit breaker.
*/
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // Normal operation
	CircuitOpen                         // Rejecting requests
	CircuitHalfOpen                     // Allowing limited probes
)

/*
CircuitBreaker prevents cascading failures by temporarily halting
requests after repeated errors and then gradually probing recovery.
*/
type CircuitBreaker struct {
	mu               sync.RWMutex
	maxFailures      int
	resetTimeout     time.Duration
	halfOpenMax      int
	failureCount     int
	state            CircuitState
	openTime         time.Time
	halfOpenAttempts int
	metrics *Metrics // TODO: use cb.metrics to drive dynamic failure thresholds / state transitions based on system load
}

/*
NewCircuitBreaker creates a breaker that opens after maxFailures
consecutive errors, waits resetTimeout, then allows halfOpenMax
probes before closing again.
*/
func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration, halfOpenMax int) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
		halfOpenMax:  halfOpenMax,
		state:        CircuitClosed,
	}
}

/*
Observe accepts current metrics (implements the Regulator interface).
*/
func (cb *CircuitBreaker) Observe(metrics *Metrics) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.metrics = metrics
}

/*
Limit returns true when requests should be rejected.
*/
func (cb *CircuitBreaker) Limit() bool {
	return !cb.Allow()
}

/*
State returns the current circuit state (read-only).
*/
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

/*
tryOpenToHalfOpen transitions from open to half-open if resetTimeout has elapsed.
Must be called with cb.mu held.
*/
func (cb *CircuitBreaker) tryOpenToHalfOpen() bool {
	if cb.state == CircuitOpen && time.Since(cb.openTime) > cb.resetTimeout {
		cb.state = CircuitHalfOpen
		cb.halfOpenAttempts = 0
		log.Info("circuit breaker renormalized to half-open")
		return true
	}
	return false
}

/*
Renormalize transitions from open to half-open if enough time has passed.
*/
func (cb *CircuitBreaker) Renormalize() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.tryOpenToHalfOpen()
}

/*
RecordFailure increments the failure counter and may open the circuit.
*/
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	if cb.failureCount >= cb.maxFailures {
		if cb.state == CircuitHalfOpen {
			cb.state = CircuitOpen
			cb.openTime = time.Now()
			cb.failureCount = 0
			log.Info("circuit breaker reopened from half-open")
		} else if cb.state == CircuitClosed {
			cb.state = CircuitOpen
			cb.openTime = time.Now()
			cb.failureCount = 0
			log.Info("circuit breaker opened")
		}
	}
}

/*
RecordSuccess resets failure state and may close the breaker.
*/
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitHalfOpen {
		cb.halfOpenAttempts++
		if cb.halfOpenAttempts >= cb.halfOpenMax {
			cb.state = CircuitClosed
			cb.failureCount = 0
			cb.halfOpenAttempts = 0
			log.Info("circuit breaker closed from half-open")
		}
	} else if cb.state == CircuitClosed {
		cb.failureCount = 0
	}
}

/*
Allow returns true when the breaker permits a request.
*/
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if cb.tryOpenToHalfOpen() {
			return true
		}
		return false
	case CircuitHalfOpen:
		return cb.halfOpenAttempts < cb.halfOpenMax
	default:
		return false
	}
}
