package pool

import (
	"context"
	"io"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestJobReadWriteCloseNilTask(t *testing.T) {
	Convey("Given a Job with nil Task", t, func() {
		job := Job{ID: "nil-task"}

		Convey("Read should error", func() {
			buf := make([]byte, 4)
			n, err := job.Read(buf)
			So(n, ShouldEqual, 0)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "nil")
		})

		Convey("Write should error", func() {
			n, err := job.Write([]byte("x"))
			So(n, ShouldEqual, 0)
			So(err, ShouldNotBeNil)
		})

		Convey("Close should error like Read/Write when Task is nil", func() {
			err := job.Close()
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "nil")
		})
	})
}

func TestNewJobComposesOptions(t *testing.T) {
	Convey("Given NewJob with multiple options", t, func() {
		ctx := context.Background()
		task := newTestTask("body")
		start := time.Now().Add(-time.Second)

		job := NewJob(
			WithID("composed"),
			WithTask(STORE, task),
			WithStartTime(start),
			WithContext(ctx),
			WithTTL(2*time.Minute),
			WithDependencies([]string{"dep-a"}),
			WithRetry(2, &ExponentialBackoff{Initial: time.Millisecond}),
			WithDependencyRetry(2, &ExponentialBackoff{Initial: time.Millisecond}),
			WithCircuitBreaker("cb", 2, time.Minute, 1),
			WithResultID("manual-result"),
		)

		Convey("Fields should reflect options", func() {
			So(job.ID, ShouldEqual, "composed")
			So(job.TaskType, ShouldEqual, STORE)
			So(job.Task, ShouldEqual, task)
			So(job.StartTime, ShouldEqual, start)
			So(job.Ctx, ShouldEqual, ctx)
			So(job.TTL, ShouldEqual, 2*time.Minute)
			So(job.Dependencies, ShouldResemble, []string{"dep-a"})
			So(job.RetryPolicy, ShouldNotBeNil)
			So(job.RetryPolicy.MaxAttempts, ShouldEqual, 2)
			So(job.DependencyRetryPolicy, ShouldNotBeNil)
			So(job.CircuitID, ShouldEqual, "cb")
			So(job.CircuitConfig, ShouldNotBeNil)
			So(job.ResultID, ShouldEqual, "manual-result")
		})
	})
}

func TestJobDelegatesToTask(t *testing.T) {
	Convey("Given a Job wrapping a task", t, func() {
		task := newTestTask("payload")
		job := NewJob(WithID("delegate"), WithTask(COMPUTE, task))

		Convey("Read should return task bytes", func() {
			buf := make([]byte, 16)
			n, err := job.Read(buf)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, len("payload"))
			So(string(buf[:n]), ShouldEqual, "payload")
			_, err = job.Read(buf)
			So(err, ShouldEqual, io.EOF)
		})

		Convey("Write should succeed", func() {
			n, err := job.Write([]byte("x"))
			So(err, ShouldBeNil)
			So(n, ShouldEqual, 1)
		})

		Convey("Close should succeed", func() {
			So(job.Close(), ShouldBeNil)
		})
	})
}

func TestWithOnDropSetsCallback(t *testing.T) {
	Convey("Given WithOnDrop", t, func() {
		var saw error
		job := Job{}
		WithOnDrop(func(err error) { saw = err })(&job)

		Convey("OnDrop should be wired", func() {
			job.OnDrop(io.EOF)
			So(saw, ShouldEqual, io.EOF)
		})
	})
}

func TestWithHalfOpenMax(t *testing.T) {
	Convey("Given WithHalfOpenMax without existing config", t, func() {
		job := Job{}
		WithHalfOpenMax(3)(&job)
		So(job.CircuitConfig, ShouldNotBeNil)
		So(job.CircuitConfig.HalfOpenMax, ShouldEqual, 3)
	})

	Convey("Given WithHalfOpenMax with existing config", t, func() {
		job := Job{CircuitConfig: &CircuitBreakerConfig{HalfOpenMax: 1}}
		WithHalfOpenMax(9)(&job)
		So(job.CircuitConfig.HalfOpenMax, ShouldEqual, 9)
	})
}

func BenchmarkNewJobWithOptions(b *testing.B) {
	task := newTestTask("payload")
	ctx := context.Background()
	start := time.Now()
	b.ReportAllocs()
	for b.Loop() {
		_ = NewJob(
			WithID("bench"),
			WithTask(COMPUTE, task),
			WithStartTime(start),
			WithContext(ctx),
			WithTTL(time.Minute),
			WithDependencies([]string{"dep"}),
			WithRetry(2, &ExponentialBackoff{Initial: time.Millisecond}),
			WithDependencyRetry(2, &ExponentialBackoff{Initial: time.Millisecond}),
			WithCircuitBreaker("cb", 2, time.Second, 1),
			WithResultID("rid"),
			WithOnDrop(func(error) {}),
		)
	}
}

func BenchmarkJobReadWriteClose(b *testing.B) {
	buffer := make([]byte, 16)
	b.ReportAllocs()
	for b.Loop() {
		job := NewJob(
			WithID("bench-rwc"),
			WithTask(COMPUTE, newTestTask("payload")),
		)

		_, _ = job.Read(buffer)
		_, _ = job.Write([]byte("x"))
		_ = job.Close()
	}
}

func BenchmarkWithHalfOpenMaxOption(b *testing.B) {
	option := WithHalfOpenMax(4)
	job := Job{}
	b.ReportAllocs()
	for b.Loop() {
		option(&job)
	}
}
