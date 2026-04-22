package compute

import (
	"context"
	"errors"
	"testing"
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"

	. "github.com/smartystreets/goconvey/convey"
)

type substrateSpy struct {
	name    string
	indices [][]uint32
}

func (spy *substrateSpy) Execute(indices []uint32) error {
	copied := append([]uint32(nil), indices...)
	spy.indices = append(spy.indices, copied)
	return nil
}

func (spy *substrateSpy) Name() string {
	if spy.name == "" {
		return "spy"
	}

	return spy.name
}

type pointerExecutorSpy struct {
	calls [][]unsafe.Pointer
}

func (spy *pointerExecutorSpy) ExecutePointers(frames []unsafe.Pointer) error {
	copied := append([]unsafe.Pointer(nil), frames...)
	spy.calls = append(spy.calls, copied)
	return nil
}

type affinityExecutorSpy struct {
	substrateSpy
	distances []uint32
	err       error
	calls     int
}

func (spy *affinityExecutorSpy) NearestAffinity(
	_ unsafe.Pointer,
	_ unsafe.Pointer,
	_ int,
) ([]uint32, error) {
	spy.calls++
	if spy.err != nil {
		return nil, spy.err
	}

	return append([]uint32(nil), spy.distances...), nil
}

func TestBackendExecutionStatsForValue(t *testing.T) {
	t.Parallel()

	Convey("Given a backend with a GPU candidate and CPU fallback", t, func() {
		gpu := &SubstrateStats{Substrate: &substrateSpy{name: "gpu"}}
		cpu := &SubstrateStats{Substrate: &substrateSpy{name: "cpu"}}
		backend := &Backend{
			ctx:      context.Background(),
			cpuStats: cpu,
			substrates: []*SubstrateStats{
				gpu,
				cpu,
			},
		}

		Convey("Heap-backed Values should route straight to CPU", func() {
			So(backend.findLowestPressureSubstrate(), ShouldEqual, cpu)
		})

		Convey("Arena-backed Values should keep the lowest-pressure substrate", func() {
			value := primitive.AllocValue()
			So(value, ShouldNotBeNil)

			defer primitive.FreeValue(value)

			So(backend.findLowestPressureSubstrate(), ShouldEqual, gpu)
		})
	})
}

func TestBackendExecuteValue(t *testing.T) {
	t.Parallel()

	Convey("Given a backend with explicit GPU and CPU executors", t, func() {
		gpuExec := &substrateSpy{name: "gpu"}
		cpuExec := &pointerExecutorSpy{}
		cpuStats := &SubstrateStats{Substrate: &substrateSpy{name: "cpu"}}
		gpuStats := &SubstrateStats{Substrate: gpuExec}
		backend := &Backend{
			ctx:      context.Background(),
			cpuStats: cpuStats,
		}

		Convey("Heap-backed Values should execute through the CPU pointer path", func() {
			value := new(primitive.Value)

			So(len(cpuExec.calls), ShouldEqual, 1)
			So(len(cpuExec.calls[0]), ShouldEqual, 1)
			So(cpuExec.calls[0][0], ShouldEqual, unsafe.Pointer(value))
			So(len(gpuExec.indices), ShouldEqual, 0)
			So(backend.findLowestPressureSubstrate(), ShouldEqual, cpuStats)
		})

		Convey("Arena-backed Values should execute on the selected substrate by slot index", func() {
			value := primitive.AllocValue()
			So(value, ShouldNotBeNil)

			defer primitive.FreeValue(value)

			slot, ok := primitive.ArenaIndex(value)
			So(ok, ShouldBeTrue)


			So(backend.findLowestPressureSubstrate(), ShouldEqual, gpuStats)
			So(len(gpuExec.indices), ShouldEqual, 1)
			So(gpuExec.indices[0], ShouldResemble, []uint32{slot})
			So(len(cpuExec.calls), ShouldEqual, 0)
		})
	})
}

func TestBackendAffinityDistances(t *testing.T) {
	t.Parallel()

	Convey("Given a backend with an affinity-capable substrate", t, func() {
		query := &[primitive.AffinityWords]uint64{0, 0, 0, 0, 0}
		candidates := [][primitive.AffinityWords]uint64{
			{1, 0, 0, 0, 0},
			{3, 0, 0, 0, 0},
		}

		Convey("AffinityDistances should use the GPU-style executor when available", func() {
			gpu := &affinityExecutorSpy{distances: []uint32{9, 1}}
			backend := &Backend{
				substrates: []*SubstrateStats{
					{Substrate: gpu},
				},
			}

			distances := backend.AffinityDistances(query, candidates)

			So(distances, ShouldResemble, []uint32{9, 1})
			So(gpu.calls, ShouldEqual, 1)
		})

		Convey("AffinityDistances should fall back to CPU distances on GPU error", func() {
			gpu := &affinityExecutorSpy{err: errors.New("boom")}
			backend := &Backend{
				substrates: []*SubstrateStats{
					{Substrate: gpu},
				},
			}

			distances := backend.AffinityDistances(query, candidates)

			So(distances, ShouldResemble, []uint32{1, 2})
			So(gpu.calls, ShouldEqual, 1)
		})
	})
}
