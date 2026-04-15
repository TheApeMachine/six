package compute

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/viz"
)

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

func TestBackendExecutePublishesWireFrame(t *testing.T) {
	convey.Convey("Execute publishes raw Value wire frames for the visualizer", t, func() {
		backend := NewBackend(context.Background())
		framesOut := make(chan []byte, 1)
		viz.SetWireValueFrameSink(func(payload []byte) {
			framesOut <- payload
		})
		defer viz.SetWireValueFrameSink(nil)

		var frame [128]uint64
		const xorNibble = kernel.OpcodeXOR
		frame[kernel.ProgramOpcodeWord] = xorNibble
		var packed uint64
		for rotation := 0; rotation < 16; rotation++ {
			packed |= xorNibble << (rotation * 4)
		}
		frame[kernel.ProgramRotTabWord] = packed
		frame[kernel.ProgramSrcAWord] = kernel.PackRegionRef(0, 16)
		frame[kernel.ProgramSrcBWord] = kernel.PackRegionRef(0, 16)
		frame[kernel.ProgramDstWord] = kernel.PackRegionRef(kernel.AffinityStartWord, 5)
		frame[kernel.IDStartWord] = 0xAB

		ptr := unsafe.Pointer(&frame[0])
		convey.So(backend.Execute([]unsafe.Pointer{ptr}), convey.ShouldBeNil)

		select {
		case payload := <-framesOut:
			ft, _, _, _, _, valueID, wire, err := viz.UnmarshalWireMessage(payload)
			convey.So(err, convey.ShouldBeNil)
			convey.So(ft, convey.ShouldEqual, byte(viz.WireFrameValue))
			convey.So(valueID, convey.ShouldEqual, uint64(0xAB))
			convey.So(len(wire), convey.ShouldEqual, 128*8)
		case <-time.After(2 * time.Second):
			t.Fatal("wire frame publish timed out")
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
