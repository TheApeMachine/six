package pool

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
)

type stubQueueExecutor struct {
	calls atomic.Int32
}

func (stub *stubQueueExecutor) Execute(frames []unsafe.Pointer) error {
	stub.calls.Add(1)

	return nil
}

type snapshotQueueExecutor struct {
	release chan struct{}
	word    chan uint64
}

func (stub *snapshotQueueExecutor) Execute(frames []unsafe.Pointer) error {
	<-stub.release

	if len(frames) == 0 || frames[0] == nil {
		stub.word <- 0

		return nil
	}

	frameWords := (*[128]uint64)(frames[0])
	stub.word <- frameWords[0]

	return nil
}

type clearingSchedulerExecutor struct{}

func (stub *clearingSchedulerExecutor) Execute(frames []unsafe.Pointer) error {
	if len(frames) == 0 || frames[0] == nil {
		return nil
	}

	frameWords := (*[128]uint64)(frames[0])
	frameWords[kernel.SchedulingNextProgramWord] = 0

	return nil
}

type settledProbeExecutor struct {
	calls atomic.Int32
}

func (stub *settledProbeExecutor) Execute(frames []unsafe.Pointer) error {
	stub.calls.Add(1)

	if len(frames) == 0 || frames[0] == nil {
		return nil
	}

	frameWords := (*[128]uint64)(frames[0])
	frameWords[kernel.SchedulingNextProgramWord] = frameWords[kernel.IDStartWord]
	frameWords[kernel.PropertiesProbeStateWord] = kernel.PackProbeState(
		kernel.CausalProbeKindHub,
		kernel.CausalProbeStatusSettled,
	)

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

	Convey("SetBackend wires the backend for Publish", t, func() {
		queue, err := NewQueue(context.Background())

		So(err, ShouldBeNil)

		stub := &stubQueueExecutor{}

		queue.SetBackend(stub)
		queue.Publish(nil)
		queue.Drain()

		So(stub.calls.Load(), ShouldEqual, 1)
		So(queue.Close(), ShouldBeNil)
	})
}

func TestQueuePublish(t *testing.T) {
	Convey("Publish snapshots the frame before async execution", t, func() {
		queue, err := NewQueue(context.Background())

		So(err, ShouldBeNil)

		stub := &snapshotQueueExecutor{
			release: make(chan struct{}),
			word:    make(chan uint64, 1),
		}

		queue.SetBackend(stub)

		value := &primitive.Value{}
		value.Set(0, 7)

		_, pubErr := queue.Publish(value)

		So(pubErr, ShouldBeNil)

		value.Set(0, 99)
		close(stub.release)

		queue.Drain()

		So(<-stub.word, ShouldEqual, uint64(7))
		So(queue.Close(), ShouldBeNil)
	})
}

func TestQueueSettledProbeStopsCascade(t *testing.T) {
	Convey("PublishTracked clears scheduler loops once the probe settles", t, func() {
		queue, err := NewQueue(context.Background())

		So(err, ShouldBeNil)

		stub := &settledProbeExecutor{}
		queue.SetBackend(stub)

		value := &primitive.Value{}
		value.Set(kernel.IDStartWord, 41)
		value.Set(
			kernel.PropertiesProbeStateWord,
			kernel.PackProbeState(kernel.CausalProbeKindHub, kernel.CausalProbeStatusActive),
		)

		So(queue.PublishTracked(value, "prompt"), ShouldBeNil)
		queue.Drain()

		So(stub.calls.Load(), ShouldEqual, 1)
		So(value[kernel.SchedulingNextProgramWord], ShouldEqual, uint64(0))
		So(queue.Close(), ShouldBeNil)
	})
}

func TestQueueExecuteNil(t *testing.T) {
	t.Parallel()

	Convey("Publish with nil queue or backend returns early", t, func() {
		var queue *Queue

		queue.Publish(nil)

		realQueue, err := NewQueue(context.Background())

		So(err, ShouldBeNil)

		_, pubErr := realQueue.Publish(nil)

		So(pubErr, ShouldNotBeNil)
		So(realQueue.Close(), ShouldBeNil)
	})
}
