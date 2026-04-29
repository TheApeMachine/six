package compute

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
substrateProbe stands in for a real CUDA/Metal/CPU substrate so the
backend's failover and store choreography can be exercised without a
device. It records call counts and either errors out or returns one
RESOLVED spawn so Sync's RESOLVED yield path is observable end-to-end.
*/
type substrateProbe struct {
	name    string
	err     error
	spawn   bool
	calls   atomic.Int64
	owners  []uint64
	closeFn func() error
}

func (probe *substrateProbe) Name() string {
	return probe.name
}

func (probe *substrateProbe) HypercubeGossip(
	value *primitive.Value,
	values []*primitive.Value,
) ([]*primitive.Value, error) {
	probe.calls.Add(1)
	probe.owners = append(probe.owners, value.ID())

	if probe.err != nil {
		return nil, probe.err
	}

	if !probe.spawn {
		return nil, nil
	}

	// Spawn arrives RESOLVED so Sync surfaces it via the yield path —
	// the test then sees end-to-end (Submit → Sync → yield) without a
	// custom drain helper poking the store.
	return []*primitive.Value{
		primitive.Emit(primitive.WithStatus(uint64(primitive.RESOLVED))),
	}, nil
}

func (probe *substrateProbe) GeometricFrame(value unsafe.Pointer, opcode uint64) bool {
	return true
}

func (probe *substrateProbe) Close() error {
	if probe.closeFn == nil {
		return nil
	}

	return probe.closeFn()
}

func TestSubmit(t *testing.T) {
	Convey("Given a backend with no values submitted yet", t, func() {
		backend := newProbeBackend(&substrateState{Substrate: &substrateProbe{name: "cpu"}})
		defer backend.Close()

		owner := primitive.Emit(primitive.WithProgram([]uint64{1}))
		defer owner.Close()

		Convey("When Submit registers a value", func() {
			err := backend.Submit(owner)
			So(err, ShouldBeNil)

			Convey("It should land in the community store keyed by ID", func() {
				stored, ok := backend.community.Load(owner.ID())
				So(ok, ShouldBeTrue)
				So(stored, ShouldEqual, owner)
			})
		})

		Convey("When Submit is called with a nil value", func() {
			err := backend.Submit(nil)

			Convey("It should refuse rather than panic", func() {
				So(err, ShouldNotBeNil)
			})
		})
	})
}

func TestSync(t *testing.T) {
	Convey("Given READY residents were submitted out of ID order", t, func() {
		probe := &substrateProbe{name: "cpu"}
		backend := newProbeBackend(&substrateState{Substrate: probe})
		defer backend.Close()

		first := primitive.Emit(primitive.WithProgram([]uint64{1}))
		second := primitive.Emit(primitive.WithProgram([]uint64{1}))
		defer first.Close()
		defer second.Close()

		So(backend.Submit(second), ShouldBeNil)
		So(backend.Submit(first), ShouldBeNil)

		Convey("When Sync walks the store", func() {
			_ = drainSync(backend)

			Convey("It should execute owners in canonical ValueID order", func() {
				So(probe.owners, ShouldResemble, []uint64{first.ID(), second.ID()})
			})
		})
	})

	Convey("Given a low-pressure substrate fails before CPU fallback", t, func() {
		gpu := &substrateProbe{name: "gpu", err: errors.New("gpu failed")}
		cpu := &substrateProbe{name: "cpu", spawn: true}
		gpuState := &substrateState{Substrate: gpu}
		cpuState := &substrateState{Substrate: cpu}
		gpuState.serviceNanos.Store(1)
		cpuState.serviceNanos.Store(100)

		backend := newProbeBackend(gpuState, cpuState)
		defer backend.Close()

		owner := primitive.Emit(primitive.WithProgram([]uint64{1}))
		defer owner.Close()

		So(backend.Submit(owner), ShouldBeNil)

		Convey("When Sync walks the store", func() {
			yielded := drainSync(backend)

			Convey("It should retry on CPU and surface the RESOLVED spawn", func() {
				So(gpu.calls.Load(), ShouldEqual, int64(1))
				So(cpu.calls.Load(), ShouldEqual, int64(1))
				So(len(yielded), ShouldEqual, 1)
				So(yielded[0].Status(), ShouldEqual, primitive.RESOLVED)

				stored, ok := backend.community.Load(yielded[0].ID())
				So(ok, ShouldBeTrue)
				So(stored, ShouldEqual, yielded[0])

				for _, value := range yielded {
					value.Close()
				}
			})
		})
	})

	Convey("Given every substrate fails", t, func() {
		gpu := &substrateProbe{name: "gpu", err: errors.New("gpu failed")}
		cpu := &substrateProbe{name: "cpu", err: errors.New("cpu failed")}
		gpuState := &substrateState{Substrate: gpu}
		cpuState := &substrateState{Substrate: cpu}

		backend := newProbeBackend(gpuState, cpuState)
		defer backend.Close()

		owner := primitive.Emit(primitive.WithProgram([]uint64{1}))
		defer owner.Close()

		So(backend.Submit(owner), ShouldBeNil)

		Convey("When Sync walks the store", func() {
			yielded := drainSync(backend)

			Convey("It should exhaust substrates and yield nothing", func() {
				So(gpu.calls.Load(), ShouldEqual, int64(1))
				So(cpu.calls.Load(), ShouldEqual, int64(1))
				So(len(yielded), ShouldEqual, 0)
				So(owner.HasProgram(), ShouldBeTrue)
			})
		})
	})
}

func TestGetCommunity(t *testing.T) {
	Convey("Given an owner with no SELECTED peers in the store", t, func() {
		backend := newProbeBackend(&substrateState{Substrate: &substrateProbe{name: "cpu"}})
		defer backend.Close()

		owner := primitive.Emit()
		bystander := primitive.Emit()
		later := primitive.Emit()
		defer owner.Close()
		defer bystander.Close()
		defer later.Close()

		So(backend.Submit(later), ShouldBeNil)
		So(backend.Submit(bystander), ShouldBeNil)
		So(backend.Submit(owner), ShouldBeNil)

		Convey("It should fall back to the entire community", func() {
			community := backend.getCommunity(owner)
			So(communityIDs(community), ShouldResemble, []uint64{
				owner.ID(),
				bystander.ID(),
				later.ID(),
			})
		})
	})

	Convey("Given peers SELECTED with reference pointing at the owner", t, func() {
		backend := newProbeBackend(&substrateState{Substrate: &substrateProbe{name: "cpu"}})
		defer backend.Close()

		owner := primitive.Emit()
		picked := primitive.Emit(
			primitive.WithStatus(uint64(primitive.SELECTED)),
		)
		picked.SetProperty(primitive.REFERENCE, owner.ID())
		other := primitive.Emit()
		defer owner.Close()
		defer picked.Close()
		defer other.Close()

		So(backend.Submit(owner), ShouldBeNil)
		So(backend.Submit(picked), ShouldBeNil)
		So(backend.Submit(other), ShouldBeNil)

		Convey("It should narrow to just the SELECTED peers", func() {
			community := backend.getCommunity(owner)
			So(len(community), ShouldEqual, 1)
			So(community[0], ShouldEqual, picked)
		})
	})
}

func TestRange(t *testing.T) {
	Convey("Given residents were submitted out of ID order", t, func() {
		backend := newProbeBackend(&substrateState{Substrate: &substrateProbe{name: "cpu"}})
		defer backend.Close()

		first := primitive.Emit()
		second := primitive.Emit()
		defer first.Close()
		defer second.Close()

		So(backend.Submit(second), ShouldBeNil)
		So(backend.Submit(first), ShouldBeNil)

		Convey("When Range visits the backend store", func() {
			var seen []uint64

			backend.Range(func(value *primitive.Value) bool {
				seen = append(seen, value.ID())

				return true
			})

			Convey("It should use canonical ValueID order", func() {
				So(seen, ShouldResemble, []uint64{first.ID(), second.ID()})
			})
		})
	})
}

func communityIDs(community []*primitive.Value) []uint64 {
	ids := make([]uint64, 0, len(community))

	for _, value := range community {
		ids = append(ids, value.ID())
	}

	return ids
}

func BenchmarkNextSubstrate(b *testing.B) {
	gpu := &substrateProbe{name: "gpu"}
	metal := &substrateProbe{name: "metal"}
	cpu := &substrateProbe{name: "cpu"}
	gpuState := &substrateState{Substrate: gpu}
	metalState := &substrateState{Substrate: metal}
	cpuState := &substrateState{Substrate: cpu}
	gpuState.serviceNanos.Store(100)
	metalState.serviceNanos.Store(50)
	cpuState.serviceNanos.Store(10)

	backend := newProbeBackend(gpuState, metalState, cpuState)
	defer backend.Close()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, _ = backend.nextSubstrate(0, 0)
	}
}

func newProbeBackend(states ...*substrateState) *Backend {
	ctx, cancel := context.WithCancel(context.Background())

	return &Backend{
		ctx:        ctx,
		cancel:     cancel,
		pool:       pool.NewPool(1),
		substrates: states,
	}
}

func drainSync(backend *Backend) []*primitive.Value {
	var yielded []*primitive.Value

	for value := range backend.Sync(context.Background()) {
		yielded = append(yielded, value.Resolved...)
	}

	return yielded
}
