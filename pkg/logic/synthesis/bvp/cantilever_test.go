package bvp

import (
	"context"
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
)

func TestCantilever(t *testing.T) {
	calc := numeric.NewCalculus()
	ctx := context.Background()

	cases := map[string]struct {
		StartPhase  int
		GoalPhase   int
		Repeats     int
		ExpectError bool
	}{
		"normal_forward_span": {
			StartPhase:  50,
			GoalPhase:   210,
			Repeats:     1,
			ExpectError: false,
		},
		"normal_backward_span": {
			StartPhase:  210,
			GoalPhase:   50,
			Repeats:     1,
			ExpectError: false,
		},
		"edge_of_field_to_edge": {
			StartPhase:  1,
			GoalPhase:   256,
			Repeats:     1,
			ExpectError: false,
		},
		"span_requires_hardening": {
			StartPhase:  15,
			GoalPhase:   88,
			Repeats:     10,
			ExpectError: false,
		},
		"zero_span_length": {
			StartPhase:  100,
			GoalPhase:   100,
			Repeats:     1,
			ExpectError: true,
		},
		"zero_start_boundary": {
			StartPhase:  0,
			GoalPhase:   100,
			Repeats:     1,
			ExpectError: true,
		},
		"zero_goal_boundary": {
			StartPhase:  100,
			GoalPhase:   0,
			Repeats:     1,
			ExpectError: true,
		},
	}

	for name, tc := range cases {
		Convey(fmt.Sprintf("Given case: %s", name), t, func() {
			macroIndex := macro.NewMacroIndexServer(
				macro.MacroIndexWithContext(ctx),
			)

			start := numeric.Phase(tc.StartPhase)
			goal := numeric.Phase(tc.GoalPhase)

			cl := NewCantileverServer(
				CantileverWithContext(ctx),
				WithMacroIndex(macroIndex),
			)

			var lastOp *macro.MacroOpcode
			var lastRot numeric.Phase
			var lastErr error

			for i := 0; i < tc.Repeats; i++ {
				lastRot, lastOp, lastErr = cl.BridgePhases(start, goal)
			}

			if tc.ExpectError {
				Convey(fmt.Sprintf("%s: bridging should return an error", name), func() {
					So(lastErr, ShouldNotBeNil)
					So(lastOp, ShouldBeNil)
				})
			} else {
				Convey(fmt.Sprintf("%s: bridging should synthesize the exact rotation delta", name), func() {
					So(lastErr, ShouldBeNil)

					invStart, err := calc.Inverse(start)
					So(err, ShouldBeNil)
					expectedRot := calc.Multiply(goal, invStart)

					So(lastRot, ShouldEqual, expectedRot)
					So(lastOp, ShouldNotBeNil)
					So(lastOp.Rotation, ShouldEqual, expectedRot)
				})

				Convey(fmt.Sprintf("%s: the generated opcode should track usage accurately", name), func() {
					So(lastOp.UseCount, ShouldEqual, tc.Repeats)

					if tc.Repeats > 5 {
						So(lastOp.Hardened, ShouldBeTrue)
					} else {
						So(lastOp.Hardened, ShouldBeFalse)
					}
				})
			}
		})
	}

	Convey("Given a Macro Index with mixed tool usages", t, func() {
		macroIndex := macro.NewMacroIndexServer(
			macro.MacroIndexWithContext(ctx),
		)

		macroIndex.RecordOpcode(25)

		for range 10 {
			macroIndex.RecordOpcode(40)
		}

		Convey("GarbageCollect should prune inefficient logic circuits", func() {
			pruned := macroIndex.GarbageCollect()
			So(pruned, ShouldEqual, 1)

			_, found := macroIndex.FindOpcode(25)
			So(found, ShouldBeFalse)

			op, found := macroIndex.FindOpcode(40)
			So(found, ShouldBeTrue)
			So(op.Hardened, ShouldBeTrue)
			So(op.UseCount, ShouldEqual, 10)
		})
	})
}
