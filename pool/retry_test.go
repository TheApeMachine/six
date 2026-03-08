package pool

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestExponentialBackoffNextDelay(t *testing.T) {
	Convey("NextDelay doubles each attempt", t, func() {
		eb := &ExponentialBackoff{Initial: 10 * time.Millisecond}
		So(eb.NextDelay(1), ShouldEqual, 10*time.Millisecond)
		So(eb.NextDelay(2), ShouldEqual, 20*time.Millisecond)
		So(eb.NextDelay(3), ShouldEqual, 40*time.Millisecond)
		So(eb.NextDelay(4), ShouldEqual, 80*time.Millisecond)
	})
}

func TestWithCircuitBreaker(t *testing.T) {
	Convey("WithCircuitBreaker sets job options", t, func() {
		j := &Job{}
		opt := WithCircuitBreaker("cb1", 3, 100*time.Millisecond)
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
