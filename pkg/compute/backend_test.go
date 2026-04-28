package compute

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

type substrateProbe struct {
	name    string
	err     error
	spawn   bool
	calls   atomic.Int64
	closeFn func() error
}

func (probe *substrateProbe) Name() string {
	return probe.name
}

func (probe *substrateProbe) HypercubeGossip(
	value *primitive.Value,
	values []*primitive.Value,
) ([]*primitive.Value, []kernel.StageRequest, error) {
	probe.calls.Add(1)

	if probe.err != nil {
		return nil, nil, probe.err
	}

	if !probe.spawn {
		return nil, nil, nil
	}

	return []*primitive.Value{primitive.Emit()}, nil, nil
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
		peer := primitive.Emit()
		defer owner.Close()
		defer peer.Close()

		Convey("When Submit executes", func() {
			err := backend.Submit(owner, []*primitive.Value{peer})
			So(err, ShouldBeNil)

			spawned := drainProbeBackend(backend)

			Convey("It should retry on CPU and retain the successful output", func() {
				So(gpu.calls.Load(), ShouldEqual, int64(1))
				So(cpu.calls.Load(), ShouldEqual, int64(1))
				So(len(spawned), ShouldEqual, 1)
				So(owner.Status(), ShouldEqual, primitive.DONE)

				for _, value := range spawned {
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
		peer := primitive.Emit()
		defer owner.Close()
		defer peer.Close()

		Convey("When Submit exhausts candidates", func() {
			err := backend.Submit(owner, []*primitive.Value{peer})
			So(err, ShouldBeNil)

			spawned := drainProbeBackend(backend)

			Convey("It should settle the owner into ERROR without executable residue", func() {
				So(gpu.calls.Load(), ShouldEqual, int64(1))
				So(cpu.calls.Load(), ShouldEqual, int64(1))
				So(len(spawned), ShouldEqual, 0)
				So(owner.Status(), ShouldEqual, primitive.ERROR)
				So(owner.SchedulingNext(), ShouldEqual, uint64(0))
				So(owner.HasProgram(), ShouldBeFalse)
			})
		})
	})
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
		queues:     make(map[QueueType]*Queue),
		pool:       pool.NewPool(1),
		substrates: states,
		cache:      sync.Map{},
		staging:    sync.Map{},
	}
}

func drainProbeBackend(backend *Backend) []*primitive.Value {
	var spawned []*primitive.Value
	for value := range backend.Sync(context.Background()) {
		spawned = append(spawned, value)
	}

	return spawned
}
