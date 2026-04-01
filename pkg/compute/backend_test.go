package compute

import (
	"context"
	"testing"
	"unsafe"

	"github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
)

func init() {
	core.Cfg.Value.Region.Program.Start = 76
	core.Cfg.Value.Region.Program.Bits = 3328
	core.Cfg.Value.Region.State.Accumulator = 62
	core.Cfg.Value.Region.State.Sequence = 61
	core.Cfg.Value.Region.Registers.FW = 74
	core.Cfg.Value.Region.Registers.PC = 75
	core.Cfg.System.BatchSize = 64
	core.Cfg.System.BatchWindow = 0
}

func makeTestBackend(t *testing.T) *Backend {
	t.Helper()
	ctx := context.Background()
	pool, err := NewPool(
		PoolWithContext(ctx),
		PoolWithProcs(4),
		PoolWithJobBuffer(64),
	)
	if err != nil {
		t.Fatal(err)
	}
	backend := NewBackend(ctx, WithPool(pool))
	if backend == nil {
		t.Fatal("NewBackend returned nil")
	}
	return backend
}

func TestQueueNilReturnsError(t *testing.T) {
	convey.Convey("Queue(nil) returns an error", t, func() {
		backend := makeTestBackend(t)
		defer backend.Shutdown()

		err := backend.Queue(nil)
		convey.So(err, convey.ShouldNotBeNil)
	})
}

func TestQueueAcceptsValidFrame(t *testing.T) {
	convey.Convey("Queue accepts a valid frame pointer", t, func() {
		backend := makeTestBackend(t)
		defer backend.Shutdown()

		var f [128]uint64
		err := backend.Queue(unsafe.Pointer(&f))
		convey.So(err, convey.ShouldBeNil)
	})
}

type recordingSubstrate struct {
	calls [][]unsafe.Pointer
}

func (substrate *recordingSubstrate) UniversalBitwise(frames []unsafe.Pointer) error {
	substrate.calls = append(
		substrate.calls,
		append([]unsafe.Pointer(nil), frames...),
	)
	return nil
}

func (substrate *recordingSubstrate) Schedule(job func(ctx context.Context) error) error {
	return job(context.Background())
}

var _ kernel.Substrate = (*recordingSubstrate)(nil)

func TestGroupFramesByProgram(t *testing.T) {
	convey.Convey("Frames are grouped by identical program regions before dispatch", t, func() {
		backend := &Backend{}
		var frameA, frameB, frameC [128]uint64

		frameA[76] = 0x1111
		frameB[76] = 0x1111
		frameC[76] = 0x2222

		groups := backend.groupFramesByProgram([]unsafe.Pointer{
			unsafe.Pointer(&frameA),
			unsafe.Pointer(&frameB),
			unsafe.Pointer(&frameC),
		})

		convey.So(len(groups), convey.ShouldEqual, 2)
		convey.So(len(groups[0]), convey.ShouldEqual, 2)
		convey.So(len(groups[1]), convey.ShouldEqual, 1)
	})
}

func TestExecuteBatchDispatchesProgramGroups(t *testing.T) {
	convey.Convey("executeBatch dispatches homogeneous program groups separately", t, func() {
		substrate := &recordingSubstrate{}
		backend := &Backend{
			ctx:      context.Background(),
			hardware: []kernel.Substrate{substrate},
			queues: map[QueueType]chan unsafe.Pointer{
				PRIORITY: make(chan unsafe.Pointer, 8),
				NORMAL:   make(chan unsafe.Pointer, 8),
			},
		}

		var frameA, frameB, frameC [128]uint64
		frameA[76] = 0x1111
		frameB[76] = 0x1111
		frameC[76] = 0x2222

		err := backend.executeBatch([]unsafe.Pointer{
			unsafe.Pointer(&frameA),
			unsafe.Pointer(&frameB),
			unsafe.Pointer(&frameC),
		})

		convey.So(err, convey.ShouldBeNil)
		convey.So(len(substrate.calls), convey.ShouldEqual, 2)
		convey.So(len(substrate.calls[0]), convey.ShouldEqual, 2)
		convey.So(len(substrate.calls[1]), convey.ShouldEqual, 1)
	})
}
