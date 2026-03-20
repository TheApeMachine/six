package pool

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestExponentialBackoffNextDelay(t *testing.T) {
	Convey("Given an ExponentialBackoff with Initial 10ms", t, func() {
		eb := &ExponentialBackoff{Initial: 10 * time.Millisecond}
		Convey("It should double the delay each attempt", func() {
			So(eb.NextDelay(1), ShouldEqual, 10*time.Millisecond)
			So(eb.NextDelay(2), ShouldEqual, 20*time.Millisecond)
			So(eb.NextDelay(3), ShouldEqual, 40*time.Millisecond)
			So(eb.NextDelay(4), ShouldEqual, 80*time.Millisecond)
		})
	})
}

func TestExponentialBackoffMaxDelayCapsBase(t *testing.T) {
	Convey("Given ExponentialBackoff with MaxDelay", t, func() {
		eb := &ExponentialBackoff{
			Initial:  100 * time.Millisecond,
			MaxDelay: 150 * time.Millisecond,
		}
		Convey("Large attempts should cap at MaxDelay", func() {
			So(eb.NextDelay(5), ShouldEqual, 150*time.Millisecond)
		})
	})
}

func TestExponentialBackoffNonPositiveAttemptClampsExponent(t *testing.T) {
	Convey("Given ExponentialBackoff", t, func() {
		eb := &ExponentialBackoff{Initial: 5 * time.Millisecond}
		So(eb.NextDelay(0), ShouldEqual, 5*time.Millisecond)
		So(eb.NextDelay(-3), ShouldEqual, 5*time.Millisecond)
	})
}

func TestWithCircuitBreaker(t *testing.T) {
	Convey("WithCircuitBreaker sets job options", t, func() {
		j := &Job{}
		opt := WithCircuitBreaker("cb1", 3, 100*time.Millisecond, 2)
		opt(j)
		So(j.CircuitID, ShouldEqual, "cb1")
		So(j.CircuitConfig, ShouldNotBeNil)
		So(j.CircuitConfig.MaxFailures, ShouldEqual, 3)
		So(j.CircuitConfig.ResetTimeout, ShouldEqual, 100*time.Millisecond)
	})
}

func TestWithRetry(t *testing.T) {
	Convey("WithRetry sets RetryPolicy", t, func() {
		j := &Job{}
		eb := &ExponentialBackoff{Initial: time.Second}
		opt := WithRetry(5, eb)
		opt(j)
		So(j.RetryPolicy, ShouldNotBeNil)
		So(j.RetryPolicy.MaxAttempts, ShouldEqual, 5)
		So(j.RetryPolicy.Strategy, ShouldEqual, eb)
	})
}

func BenchmarkExponentialBackoffNextDelay(b *testing.B) {
	eb := &ExponentialBackoff{Initial: 10 * time.Millisecond, MaxDelay: time.Second}
	b.ReportAllocs()
	var attempt int
	for b.Loop() {
		attempt++
		eb.NextDelay(attempt % 16)
	}
}

func BenchmarkWithCircuitBreaker(b *testing.B) {
	for i := 0; i < b.N; i++ {
		job := &Job{}
		opt := WithCircuitBreaker("cb1", 3, 100*time.Millisecond, 2)
		opt(job)
	}
}

func BenchmarkWithRetry(b *testing.B) {
	eb := &ExponentialBackoff{Initial: time.Second}
	for i := 0; i < b.N; i++ {
		job := &Job{}
		opt := WithRetry(5, eb)
		opt(job)
	}
}
