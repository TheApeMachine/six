package lsm

import (
	"context"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/synthesis/goal"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

func TestWavefrontBridging(t *testing.T) {
	gc.Convey("Given a Spatial Index with a missing data span", t, func() {
		// Create index with "A B _ D" (we skip inserting 'C')
		idx := NewSpatialIndexServer(WithContext(context.Background()))
		ctx := context.Background()
		calc := numeric.NewCalculus()
		mortonObj := data.NewMortonCoder()

		state := numeric.Phase(1)

		// Insert 'A' at pos 0
		stateA := calc.Multiply(state, calc.Power(3, uint32('A')))
		chordA := data.BaseChord('A')
		chordA.Set(int(stateA))
		idx.insertSync(mortonObj.Pack(0, 'A'), chordA, data.MustNewChord())

		// Insert 'B' at pos 1
		stateB := calc.Multiply(stateA, calc.Power(3, uint32('B')))
		chordB := data.BaseChord('B')
		chordB.Set(int(stateB))
		idx.insertSync(mortonObj.Pack(1, 'B'), chordB, data.MustNewChord())

		// We intentionally DO NOT insert 'C' at pos 2
		// The mathematical "Goal" phase for D would be: stateA -> B -> C -> D
		stateC := calc.Multiply(stateB, calc.Power(3, uint32('C')))
		stateD := calc.Multiply(stateC, calc.Power(3, uint32('D')))

		// Insert 'D' at pos 3 (gap at pos 2)
		chordD := data.BaseChord('D')
		chordD.Set(int(stateD))
		idx.insertSync(mortonObj.Pack(3, 'D'), chordD, data.MustNewChord())

		gc.Convey("Wavefront without Frustration Engine dies at the gap", func() {
			promptChord := data.BaseChord('A')

			wf := NewWavefront(idx, WavefrontWithMaxDepth(5))
			results := wf.Search(promptChord, nil, nil)

			// It should only manage to go A -> B (2 steps, length 2)
			// because position 2 is empty
			gc.So(len(results), gc.ShouldBeGreaterThan, 0)
			best := results[0]
			gc.So(len(best.Path), gc.ShouldEqual, 2)
			gc.So(best.Phase, gc.ShouldEqual, stateB)
		})

		gc.Convey("Wavefront WITH Frustration Engine discovers a bridging tool and leaps the gap", func() {
			macroIndex := macro.NewMacroIndexServer(
				macro.MacroIndexWithContext(ctx),
			)

			// We give the index the mathematical tool to bridge from B to D:
			// Rot = Goal * Inverse(Start)  --> Rot = stateD * Inverse(stateB)
			invB, _ := calc.Inverse(stateB)
			targetRotation := calc.Multiply(stateD, invB)

			// Let's ensure the library knows this tool is viable
			macroIndex.RecordOpcode(targetRotation)
			for range 5 {
				macroIndex.RecordOpcode(targetRotation) // Harden it
			}

			// We attach the frustration engine to the wavefront, targeting stateD
			fe := goal.NewFrustrationEngineServer(
				goal.FrustrationWithContext(ctx),
				goal.WithSharedIndex(macroIndex),
			)
			wf := NewWavefront(idx,
				WavefrontWithMaxDepth(5),
				WavefrontWithFrustrationEngine(fe, stateD),
			)

			promptChord := data.BaseChord('A')
			results := wf.Search(promptChord, nil, nil)

			gc.So(len(results), gc.ShouldBeGreaterThan, 0)

			// The bridge deposits phase stateD at pos 2. The wavefront then
			// continues to pos 3 where 'D' is stored, advancing phase one more
			// step: stateD * G^D. This validates the bridge enabled reaching
			// the otherwise-dead data at pos 3.
			expectedFinal := calc.Multiply(stateD, calc.Power(3, uint32('D')))
			best := results[0]
			gc.So(best.Phase, gc.ShouldEqual, expectedFinal)

			// Validate that the Macro Index recorded successful uses of the tool
			op, found := macroIndex.FindOpcode(targetRotation)
			gc.So(found, gc.ShouldBeTrue)
			gc.So(op.Hardened, gc.ShouldBeTrue)
		})
	})
}
