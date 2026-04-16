package compute

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
)

type shutdownStubSubstrate struct {
	shutdowns atomic.Int64
}

func (substrate *shutdownStubSubstrate) Execute(indices []uint32) error {
	_ = indices
	return nil
}

func (substrate *shutdownStubSubstrate) Name() string {
	return "shutdown-stub"
}

func (substrate *shutdownStubSubstrate) Shutdown() {
	substrate.shutdowns.Add(1)
}

func stubTwoSubstrateBackend() *Backend {
	return &Backend{
		states: []*substrateState{
			{idx: 0, emaNanos: atomic.Int64{}},
			{idx: 1, emaNanos: atomic.Int64{}},
		},
		transferPenalty: 1 << 20,
		exploreEvery:    0,
	}
}

func TestBackendEffectiveTransferPenaltyShrinksWhenResidentSlow(t *testing.T) {
	convey.Convey("Given a frame resident on a slow substrate", t, func() {
		backend := stubTwoSubstrateBackend()
		backend.states[0].emaNanos.Store(8000)
		backend.states[1].emaNanos.Store(5)

		pen := backend.effectiveTransferPenalty(0, backend.states[1], false)

		convey.So(pen, convey.ShouldBeGreaterThan, int64(0))
		convey.So(pen, convey.ShouldBeLessThan, backend.transferPenalty)
	})

	convey.Convey("Exploration zeros the penalty", t, func() {
		backend := stubTwoSubstrateBackend()
		backend.states[0].emaNanos.Store(8000)
		backend.states[1].emaNanos.Store(5)

		convey.So(
			backend.effectiveTransferPenalty(0, backend.states[1], true),
			convey.ShouldEqual,
			int64(0),
		)
	})

	convey.Convey("Symmetric substrates keep the full toll", t, func() {
		backend := stubTwoSubstrateBackend()
		backend.states[0].emaNanos.Store(100)
		backend.states[1].emaNanos.Store(100)

		convey.So(
			backend.effectiveTransferPenalty(0, backend.states[1], false),
			convey.ShouldEqual,
			backend.transferPenalty,
		)
	})
}

func TestBackendPickPrefersFastSubstrateUnderAmortizedPenalty(t *testing.T) {
	convey.Convey("Pick migrates off a slow resident substrate", t, func() {
		backend := stubTwoSubstrateBackend()
		backend.transferPenalty = 500_000
		backend.states[0].inflight.Store(1)
		backend.states[1].inflight.Store(1)
		backend.states[0].emaNanos.Store(200_000)
		backend.states[1].emaNanos.Store(100)

		var frame [128]uint64

		frame[kernel.FrameMetaResidencyWord] = 1

		chosen := backend.pick([]unsafe.Pointer{unsafe.Pointer(&frame)})

		convey.So(chosen, convey.ShouldEqual, backend.states[1])
	})
}

func TestBackendPickExplorationIgnoresPenalty(t *testing.T) {
	convey.Convey("Periodic exploration drops the migration toll", t, func() {
		backend := stubTwoSubstrateBackend()
		backend.exploreEvery = 1
		backend.transferPenalty = 1 << 30
		backend.states[0].emaNanos.Store(10)
		backend.states[1].emaNanos.Store(12)

		var frame [128]uint64

		frame[kernel.FrameMetaResidencyWord] = 1

		chosen := backend.pick([]unsafe.Pointer{unsafe.Pointer(&frame)})

		convey.So(chosen, convey.ShouldEqual, backend.states[0])
	})
}

func TestBackendCloseShutsDownSubstrates(t *testing.T) {
	convey.Convey("Close cancels the backend and shuts down substrates that support it", t, func() {
		first := &shutdownStubSubstrate{}
		second := &shutdownStubSubstrate{}

		ctx, cancel := context.WithCancel(context.Background())
		backend := &Backend{
			ctx:    ctx,
			cancel: cancel,
			states: []*substrateState{
				{idx: 0, substrate: first},
				{idx: 1, substrate: second},
			},
		}

		convey.So(backend.Close(), convey.ShouldBeNil)
		convey.So(first.shutdowns.Load(), convey.ShouldEqual, 1)
		convey.So(second.shutdowns.Load(), convey.ShouldEqual, 1)

		select {
		case <-backend.ctx.Done():
		case <-time.After(2 * time.Second):
			t.Fatal("backend context was not canceled")
		}
	})
}

func BenchmarkBackendPick(b *testing.B) {
	backend := stubTwoSubstrateBackend()
	backend.states[0].emaNanos.Store(5000)
	backend.states[1].emaNanos.Store(10)
	backend.states[0].inflight.Store(1)
	backend.states[1].inflight.Store(1)

	var frame [128]uint64

	frame[kernel.FrameMetaResidencyWord] = 1

	ptrs := []unsafe.Pointer{unsafe.Pointer(&frame)}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_ = backend.pick(ptrs)
	}
}
