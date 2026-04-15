package vm

import (
	"context"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

func hasNonZeroAffinity(value *primitive.Value, affinityStart int, affinityWords int) bool {
	if value == nil {
		return false
	}

	for offset := 0; offset < affinityWords; offset++ {
		if (*value)[affinityStart+offset] != 0 {
			return true
		}
	}

	return false
}

/*
TestOrchestratorSubmitStep verifies that the in-value scheduler word is treated
as first-class control flow. A resident next-self program must be re-submitted
only when no config rule matches, otherwise the Go rule engine is shadowing the
Value's own program.
*/
func TestOrchestratorSubmitStep(t *testing.T) {
	Convey("Given a settled Value whose scheduler word points to itself", t, func() {
		dispatched := make(chan *programmer.Executable, 1)

		queue, err := pool.NewQueue(context.Background(), func(executable *programmer.Executable) {
			dispatched <- executable
		})

		So(err, ShouldBeNil)
		defer func() {
			So(queue.Close(), ShouldBeNil)
		}()

		orchestrator, err := NewOrchestrator(context.Background(), nil, queue)

		So(err, ShouldBeNil)
		defer func() {
			So(orchestrator.Close(), ShouldBeNil)
		}()

		values, err := primitive.NewValue([]byte("resident loop"))

		So(err, ShouldBeNil)
		So(len(values), ShouldBeGreaterThan, 0)

		value := values[0]
		defer value.Close()

		affinityStart, _ := primitive.AffinityRegion.WordExtent()
		value.Set(affinityStart, 1)
		value.Set(kernel.SchedulingNextProgramWord, value.ID())

		orchestrator.submitStep(value)

		Convey("It should reschedule the resident program onto the pool", func() {
			select {
			case executable := <-dispatched:
				So(executable, ShouldNotBeNil)
				So(executable.Value(), ShouldEqual, value)
			case <-time.After(2 * time.Second):
				t.Fatal("pool dispatch timed out")
			}
		})
	})

	Convey("Given a Value that still matches a config rule", t, func() {
		dispatched := make(chan *programmer.Executable, 1)

		queue, err := pool.NewQueue(context.Background(), func(executable *programmer.Executable) {
			dispatched <- executable
		})

		So(err, ShouldBeNil)
		defer func() {
			So(queue.Close(), ShouldBeNil)
		}()

		orchestrator, err := NewOrchestrator(context.Background(), nil, queue)

		So(err, ShouldBeNil)
		defer func() {
			So(orchestrator.Close(), ShouldBeNil)
		}()

		values, err := primitive.NewValue([]byte("link then affinity"))

		So(err, ShouldBeNil)
		So(len(values), ShouldBeGreaterThan, 0)

		value := values[0]
		defer value.Close()

		prevStart, _ := primitive.PrevRegion.WordExtent()
		nextStart, _ := primitive.NextRegion.WordExtent()
		value.Set(prevStart, 11)
		value.Set(nextStart, 22)
		value.Set(kernel.SchedulingNextProgramWord, value.ID())

		orchestrator.submitStep(value)

		Convey("It should prefer the affinity rule before resident re-execution", func() {
			select {
			case executable := <-dispatched:
				So(executable, ShouldNotBeNil)
				So(executable.Value(), ShouldEqual, value)
				So(executable.IsResidentProgram(), ShouldBeFalse)
			case <-time.After(2 * time.Second):
				t.Fatal("pool dispatch timed out")
			}
		})
	})
}

/*
TestOrchestratorPublishLinkAffinityRoute verifies the prerequisite chain needed
before any community-local ValueID addressing can exist: publish must stage a
real link, link must write exact prev/next IDs in-band, affinity must be
computed in-band, and only then may routing assign Values into communities.
*/
func TestOrchestratorPublishLinkAffinityRoute(t *testing.T) {
	Convey("Given two fresh Values published through a live backend", t, func() {
		ctx := context.Background()
		backend := compute.NewBackend(ctx)
		queue, err := pool.NewQueue(ctx, backend.Dispatch)

		So(err, ShouldBeNil)
		defer func() {
			So(queue.Close(), ShouldBeNil)
		}()

		orchestrator, err := NewOrchestrator(ctx, nil, queue)

		So(err, ShouldBeNil)
		defer func() {
			So(orchestrator.Close(), ShouldBeNil)
		}()

		first, err := primitive.FirstSegment(primitive.NewValue([]byte("first")))
		So(err, ShouldBeNil)
		defer first.Close()

		second, err := primitive.FirstSegment(primitive.NewValue([]byte("second")))
		So(err, ShouldBeNil)
		defer second.Close()

		_, publishErr := orchestrator.Publish(first, second)
		So(publishErr, ShouldBeNil)

		prevStart, _ := primitive.PrevRegion.WordExtent()
		nextStart, _ := primitive.NextRegion.WordExtent()
		affinityStart, affinityWords := primitive.AffinityRegion.WordExtent()

		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			linked := (*first)[prevStart] == 0 && (*first)[nextStart] == second.ID() &&
				(*second)[prevStart] == first.ID() && (*second)[nextStart] == 0

			assigned := 0
			for _, peer := range orchestrator.router.route {
				community, ok := peer.Dst().(*geometry.Field)
				if !ok || community == nil {
					continue
				}
				assigned += len(community.Values)
			}

			if linked && hasNonZeroAffinity(first, affinityStart, affinityWords) && hasNonZeroAffinity(second, affinityStart, affinityWords) && assigned >= 2 {
				break
			}

			time.Sleep(10 * time.Millisecond)
		}

		Convey("Publish should link neighbors, compute affinity, and route both Values", func() {
			So((*first)[prevStart], ShouldEqual, uint64(0))
			So((*first)[nextStart], ShouldEqual, second.ID())
			So((*second)[prevStart], ShouldEqual, first.ID())
			So((*second)[nextStart], ShouldEqual, uint64(0))
			So(hasNonZeroAffinity(first, affinityStart, affinityWords), ShouldBeTrue)
			So(hasNonZeroAffinity(second, affinityStart, affinityWords), ShouldBeTrue)

			assigned := 0
			for _, peer := range orchestrator.router.route {
				community, ok := peer.Dst().(*geometry.Field)
				if !ok || community == nil {
					continue
				}
				assigned += len(community.Values)
			}
			So(assigned, ShouldBeGreaterThanOrEqualTo, 2)
		})
	})
}

func BenchmarkSubmitStep(b *testing.B) {
	ctx := context.Background()
	queue, err := pool.NewQueue(ctx, func(*programmer.Executable) {})
	if err != nil {
		b.Fatal(err)
	}
	defer queue.Close()

	orchestrator, err := NewOrchestrator(ctx, nil, queue)
	if err != nil {
		b.Fatal(err)
	}
	defer orchestrator.Close()

	values, err := primitive.NewValue([]byte("bench submit"))
	if err != nil || len(values) == 0 {
		b.Fatal(err)
	}
	value := values[0]
	defer value.Close()

	affinityStart, _ := primitive.AffinityRegion.WordExtent()
	value.Set(affinityStart, 1)
	value.Set(kernel.SchedulingNextProgramWord, value.ID())

	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		orchestrator.submitStep(value)
	}
}

func BenchmarkPublish(b *testing.B) {
	ctx := context.Background()
	queue, err := pool.NewQueue(ctx, func(*programmer.Executable) {})
	if err != nil {
		b.Fatal(err)
	}
	defer queue.Close()

	orchestrator, err := NewOrchestrator(ctx, nil, queue)
	if err != nil {
		b.Fatal(err)
	}
	defer orchestrator.Close()

	first, err := primitive.FirstSegment(primitive.NewValue([]byte("bench first")))
	if err != nil {
		b.Fatal(err)
	}
	defer first.Close()

	second, err := primitive.FirstSegment(primitive.NewValue([]byte("bench second")))
	if err != nil {
		b.Fatal(err)
	}
	defer second.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		_, _ = orchestrator.Publish(first, second)
	}
}
