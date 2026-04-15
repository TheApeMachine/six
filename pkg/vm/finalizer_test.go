package vm

import (
	"context"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/programmer"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestActionFinalizer(t *testing.T) {
	Convey("Given a generic post-ALU action finalizer", t, func() {
		originalRules := core.Cfg.Finalizers
		Reset(func() {
			core.Cfg.Finalizers = originalRules
		})

		dispatched := make(chan *programmer.Executable, 8)

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

		Convey("It should reprogram the current Value from a value-scope rule", func() {
			core.Cfg.Finalizers = []core.FinalizerRuleConfig{
				{
					Name:    "value-reprogram",
					Scope:   valueFinalizerScope,
					Regions: map[string]bool{"signals": true},
					Actions: []core.FinalizerActionConfig{
						{Type: "reprogram", Program: "affinity"},
					},
				},
			}

			value, err := primitive.FirstSegment(primitive.NewValue([]byte("finalize")))
			So(err, ShouldBeNil)
			defer value.Close()

			signalsStart, _ := primitive.SignalsRegion.WordExtent()
			value.Set(signalsStart, 1)

			consumed := orchestrator.finalizer.FinalizeValue(value)
			So(consumed, ShouldBeTrue)

			select {
			case executable := <-dispatched:
				So(executable, ShouldNotBeNil)
				So(executable.Value(), ShouldEqual, value)
			case <-time.After(2 * time.Second):
				t.Fatal("expected reprogrammed executable")
			}
		})

		Convey("It should emit an ephemeral clone from a field-scope rule", func() {
			core.Cfg.Finalizers = []core.FinalizerRuleConfig{
				{
					Name:       "community-emit",
					Scope:      communityFinalizerScope,
					MinMembers: 1,
					Actions: []core.FinalizerActionConfig{
						{
							Type:    "emit",
							Program: "beam_swarm_step",
							TTL:     1,
							Copies: []core.FinalizerCopyConfig{
								{
									Source:      "field.affinity[0,5]",
									Destination: "gradient[0,5]",
								},
							},
						},
					},
				},
			}

			value, err := primitive.FirstSegment(primitive.NewValue([]byte("field emit")))
			So(err, ShouldBeNil)
			defer value.Close()

			community := geometry.NewCommunityField(geometry.Mod8191)
			community.Values = append(community.Values, value)
			community.Affinity = []uint64{1, 2, 3, 4, 1}

			consumed := orchestrator.finalizer.FinalizeField(
				communityFinalizerScope,
				value,
				community,
			)
			So(consumed, ShouldBeTrue)

			select {
			case executable := <-dispatched:
				So(executable, ShouldNotBeNil)
				So(executable.Value(), ShouldNotEqual, value)
				So(executable.Value().ID(), ShouldNotEqual, value.ID())
				So((*executable.Value())[51], ShouldEqual, uint64(1))
				So((*executable.Value())[120], ShouldEqual, value.ID())
			case <-time.After(2 * time.Second):
				t.Fatal("expected emitted executable")
			}
		})
	})
}
