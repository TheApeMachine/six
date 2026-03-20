package pool

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestCircuitBreakerLimitMatchesAllow(t *testing.T) {
	Convey("Given a CircuitBreaker", t, func() {
		cb := NewCircuitBreaker(2, 100*time.Millisecond, 1)

		Convey("Limit should be the negation of Allow while closed", func() {
			So(cb.Limit(), ShouldEqual, !cb.Allow())
		})
	})
}

func TestCircuitBreakerStateObservation(t *testing.T) {
	Convey("Given a CircuitBreaker", t, func() {
		cb := NewCircuitBreaker(1, 200*time.Millisecond, 1)

		Convey("Initial state should be closed", func() {
			So(cb.State(), ShouldEqual, CircuitClosed)
		})

		Convey("After failures it should open", func() {
			cb.RecordFailure()
			So(cb.State(), ShouldEqual, CircuitOpen)
		})
	})
}

func TestCircuitBreakerObserveDoesNotBreakAllow(t *testing.T) {
	Convey("Given CircuitBreaker and Metrics", t, func() {
		cb := NewCircuitBreaker(2, time.Second, 1)
		m := NewMetrics()

		Convey("Observe should keep closed breaker permissive", func() {
			cb.Observe(m)
			So(cb.Allow(), ShouldBeTrue)
		})
	})
}

func TestCircuitBreakerRenormalizeFromOpen(t *testing.T) {
	Convey("Given an open breaker past reset timeout", t, func() {
		cb := NewCircuitBreaker(1, 50*time.Millisecond, 2)
		cb.RecordFailure()
		So(cb.State(), ShouldEqual, CircuitOpen)

		time.Sleep(80 * time.Millisecond)
		cb.Renormalize()

		So(cb.State(), ShouldEqual, CircuitHalfOpen)
	})
}

func BenchmarkCircuitBreakerAllow(b *testing.B) {
	cb := NewCircuitBreaker(8, time.Second, 4)
	b.ReportAllocs()
	for b.Loop() {
		cb.Allow()
	}
}

func BenchmarkCircuitBreakerRecordFailureAndSuccess(b *testing.B) {
	cb := NewCircuitBreaker(4, time.Second, 2)
	b.ReportAllocs()
	for b.Loop() {
		cb.RecordFailure()
		cb.RecordSuccess()
	}
}

func BenchmarkCircuitBreakerObserveLimitState(b *testing.B) {
	cb := NewCircuitBreaker(2, time.Second, 1)
	metrics := NewMetrics()
	cb.Observe(metrics)
	b.ReportAllocs()
	for b.Loop() {
		_ = cb.Limit()
		_ = cb.State()
	}
}

func BenchmarkCircuitBreakerRenormalizeFromOpen(b *testing.B) {
	cb := NewCircuitBreaker(1, time.Hour, 1)
	cb.RecordFailure()
	b.ReportAllocs()
	for b.Loop() {
		cb.Renormalize()
	}
}
