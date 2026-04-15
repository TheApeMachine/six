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

func TestLifecycleEngine(t *testing.T) {
	Convey("Given a unified lifecycle engine", t, func() {
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

		Convey("It should select the boot firmware generically", func() {
			value, err := primitive.FirstSegment(primitive.NewValue([]byte("boot selection")))
			So(err, ShouldBeNil)
			defer value.Close()

			So(orchestrator.lifecycle.SelectProgram(value), ShouldEqual, "link")

			prevStart, _ := primitive.PrevRegion.WordExtent()
			value.Set(prevStart, 1)
			So(orchestrator.lifecycle.SelectProgram(value), ShouldEqual, "affinity")
		})

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

			value, err := primitive.FirstSegment(primitive.NewValue([]byte("lifecycle finalize")))
			So(err, ShouldBeNil)
			defer value.Close()

			signalsStart, _ := primitive.SignalsRegion.WordExtent()
			value.Set(signalsStart, 1)

			consumed := orchestrator.lifecycle.FinalizeValue(value)
			So(consumed, ShouldBeTrue)

			select {
			case executable := <-dispatched:
				So(executable, ShouldNotBeNil)
				So(executable.Value(), ShouldEqual, value)
			case <-time.After(2 * time.Second):
				t.Fatal("expected lifecycle reprogrammed executable")
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

			value, err := primitive.FirstSegment(primitive.NewValue([]byte("lifecycle field emit")))
			So(err, ShouldBeNil)
			defer value.Close()

			community := geometry.NewCommunityField(geometry.Mod8191)
			community.Values = append(community.Values, value)
			community.Affinity = []uint64{1, 2, 3, 4, 1}

			consumed := orchestrator.lifecycle.FinalizeField(
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
			case <-time.After(2 * time.Second):
				t.Fatal("expected lifecycle emitted executable")
			}
		})
	})
}
