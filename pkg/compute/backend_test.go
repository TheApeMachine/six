package compute

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
substrateProbe implements kernel.Substrate for tests so HypercubeGossip paths
run without a device. It records owner IDs per call and optionally errors or
returns one RESOLVED spawn for yield-path coverage.
*/
type substrateProbe struct {
	name     string
	err      error
	spawn    bool
	calls    atomic.Int64
	ownersMu sync.Mutex
	owners   []uint64
	closeFn  func() error
}

func (probe *substrateProbe) Name() string {
	return probe.name
}

func (probe *substrateProbe) HypercubeGossip(
	value *primitive.Value,
	values []*primitive.Value,
) ([]*primitive.Value, error) {
	probe.calls.Add(1)
	probe.ownersMu.Lock()
	probe.owners = append(probe.owners, value.ID())
	probe.ownersMu.Unlock()

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
		backend := newProbeBackend(&substrateProbe{name: "cpu"})
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
		backend := newProbeBackend(probe)
		defer backend.Close()

		first := primitive.Emit(primitive.WithProgram([]uint64{1}))
		second := primitive.Emit(primitive.WithProgram([]uint64{1}))
		defer first.Close()
		defer second.Close()

		So(backend.Submit(second), ShouldBeNil)
		So(backend.Submit(first), ShouldBeNil)

		Convey("When Sync walks the store", func() {
			_ = drainSync(backend)

			Convey("It should execute each owner exactly once", func() {
				got := append([]uint64(nil), probe.owners...)
				sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })

				want := []uint64{first.ID(), second.ID()}
				sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })

				So(got, ShouldResemble, want)
			})
		})
	})

	Convey("Given Sync dispatches on the tail substrate only", t, func() {
		gpu := &substrateProbe{name: "gpu", err: errors.New("gpu failed")}
		cpu := &substrateProbe{name: "cpu", spawn: true}

		backend := newProbeBackend(gpu, cpu)
		defer backend.Close()

		owner := primitive.Emit(primitive.WithProgram([]uint64{1}))
		defer owner.Close()

		So(backend.Submit(owner), ShouldBeNil)

		Convey("When Sync walks the store", func() {
			yielded := drainSync(backend)

			Convey("It should call only the last substrate and surface the RESOLVED spawn", func() {
				So(gpu.calls.Load(), ShouldEqual, int64(0))
				So(cpu.calls.Load(), ShouldEqual, int64(1))
				So(len(yielded), ShouldEqual, 1)
				So(yielded[0].Status(), ShouldEqual, primitive.RESOLVED)

				for _, value := range yielded {
					value.Close()
				}
			})
		})
	})

	Convey("Given the tail substrate fails", t, func() {
		gpu := &substrateProbe{name: "gpu", err: errors.New("gpu failed")}
		cpu := &substrateProbe{name: "cpu", err: errors.New("cpu failed")}

		backend := newProbeBackend(gpu, cpu)
		defer backend.Close()

		owner := primitive.Emit(primitive.WithProgram([]uint64{1}))
		defer owner.Close()

		So(backend.Submit(owner), ShouldBeNil)

		Convey("When Sync walks the store", func() {
			yielded := drainSync(backend)

			Convey("It should surface nothing and mark the owner errored", func() {
				So(gpu.calls.Load(), ShouldEqual, int64(0))
				So(cpu.calls.Load(), ShouldEqual, int64(1))
				So(len(yielded), ShouldEqual, 0)
				So(owner.Status(), ShouldEqual, primitive.ERROR)
			})
		})
	})
}

func newProbeBackend(substrates ...kernel.Substrate) *Backend {
	ctx, cancel := context.WithCancel(context.Background())

	return &Backend{
		ctx:        ctx,
		cancel:     cancel,
		pool:       pool.NewPool(1),
		substrates: substrates,
	}
}

func drainSync(backend *Backend) []*primitive.Value {
	var yielded []*primitive.Value

	for value := range backend.Sync(context.Background()) {
		yielded = append(yielded, value.Resolved...)
	}

	return yielded
}
