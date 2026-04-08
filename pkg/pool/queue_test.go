package pool

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

type stubQueueExecutor struct {
	calls atomic.Int32
}

func (stub *stubQueueExecutor) CompileAndExecute(program any) error {
	stub.calls.Add(1)

	return nil
}

func TestNewQueue(t *testing.T) {
	t.Parallel()

	Convey("NewQueue allocates rings and pool", t, func() {
		queue, err := NewQueue(context.Background())

		So(err, ShouldBeNil)
		So(queue, ShouldNotBeNil)
		So(queue.Close(), ShouldBeNil)
	})
}

func TestQueueClose(t *testing.T) {
	t.Parallel()

	Convey("Close cancels the queue context", t, func() {
		queue, err := NewQueue(context.Background())

		So(err, ShouldBeNil)
		So(queue.Close(), ShouldBeNil)
	})
}

func TestQueueError(t *testing.T) {
	t.Parallel()

	Convey("Error exposes construction errors", t, func() {
		queue, err := NewQueue(context.Background())

		So(err, ShouldBeNil)
		So(queue.Error(), ShouldBeNil)
		So(queue.Close(), ShouldBeNil)
	})
}

func TestQueueSubmit(t *testing.T) {
	Convey("Submit runs work on the internal pool", t, func() {
		var ran atomic.Bool
		var wait sync.WaitGroup

		queue, err := NewQueue(context.Background())

		So(err, ShouldBeNil)

		wait.Add(1)

		queue.Submit(func() {
			ran.Store(true)
			wait.Done()
		})

		wait.Wait()

		So(ran.Load(), ShouldBeTrue)

		So(queue.Close(), ShouldBeNil)
	})
}

func TestQueueSubmitNil(t *testing.T) {
	t.Parallel()

	Convey("Submit on nil is a no-op", t, func() {
		var queue *Queue

		queue.Submit(func() {})
	})
}

func TestQueueSubmitTracked(t *testing.T) {
	Convey("SubmitTracked participates in Drain accounting", t, func() {
		var ran atomic.Int32

		queue, err := NewQueue(context.Background())

		So(err, ShouldBeNil)

		for range 8 {
			queue.SubmitTracked(func() {
				ran.Add(1)
			})
		}

		queue.Drain()
		So(ran.Load(), ShouldEqual, 8)
		So(queue.Close(), ShouldBeNil)
	})
}

func TestQueueSubmitTrackedNil(t *testing.T) {
	t.Parallel()

	Convey("SubmitTracked ignores nil queue or task", t, func() {
		var queue *Queue

		queue.SubmitTracked(nil)

		realQueue, err := NewQueue(context.Background())

		So(err, ShouldBeNil)

		realQueue.SubmitTracked(nil)
		So(realQueue.Close(), ShouldBeNil)
	})
}

func TestQueueSetBackend(t *testing.T) {
	t.Parallel()

	Convey("SetBackend wires the executor for Execute", t, func() {
		queue, err := NewQueue(context.Background())

		So(err, ShouldBeNil)

		stub := &stubQueueExecutor{}

		queue.SetBackend(stub)
		queue.Execute(struct{}{})
		queue.Drain()

		So(stub.calls.Load(), ShouldEqual, 1)
		So(queue.Close(), ShouldBeNil)
	})
}

func TestQueueExecuteNil(t *testing.T) {
	t.Parallel()

	Convey("Execute with nil queue or backend returns early", t, func() {
		var queue *Queue

		queue.Execute(struct{}{})

		realQueue, err := NewQueue(context.Background())

		So(err, ShouldBeNil)

		realQueue.Execute(struct{}{})
		So(realQueue.Close(), ShouldBeNil)
	})
}

func TestQueueDrainNil(t *testing.T) {
	t.Parallel()

	Convey("Drain on nil is safe", t, func() {
		var queue *Queue

		queue.Drain()
	})
}

func TestQueueExecuteSync(t *testing.T) {
	t.Parallel()

	Convey("ExecuteSync runs the backend in the calling goroutine", t, func() {
		queue, err := NewQueue(context.Background())

		So(err, ShouldBeNil)

		stub := &stubQueueExecutor{}

		queue.SetBackend(stub)
		So(queue.ExecuteSync(struct{}{}), ShouldBeNil)
		So(stub.calls.Load(), ShouldEqual, 1)

		var nilQueue *Queue

		So(nilQueue.ExecuteSync(struct{}{}), ShouldBeNil)

		queue.SetBackend(nil)
		So(queue.ExecuteSync(struct{}{}), ShouldBeNil)

		So(queue.Close(), ShouldBeNil)
	})
}

func BenchmarkQueueSubmit(b *testing.B) {
	queue, err := NewQueue(context.Background())

	if err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() {
		_ = queue.Close()
	})

	b.ResetTimer()

	for b.Loop() {
		queue.Submit(func() {})
	}
}
