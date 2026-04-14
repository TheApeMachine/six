package pool

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
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

type completionExecutor struct{}

func (stub *completionExecutor) Execute(frames []unsafe.Pointer) error {
	if len(frames) == 0 || frames[0] == nil {
		return nil
	}

	frameWords := (*[128]uint64)(frames[0])
	frameWords[0] = 77

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
