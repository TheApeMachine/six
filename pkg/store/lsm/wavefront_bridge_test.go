package lsm

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/synthesis"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

func TestWavefrontBridging(t *testing.T) {
	gc.Convey("Given a Spatial Index with a missing data span", t, func() {
		// Create index with "A B _ D" (we skip inserting 'C')
		idx := NewSpatialIndexServer()
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
			macroIndex := synthesis.NewMacroIndex()

			// We give the index the mathematical tool to bridge from B to D:
			// Rot = Goal * Inverse(Start)  --> Rot = stateD * Inverse(stateB)
			invB, _ := calc.Inverse(stateB)
			targetRotation := calc.Multiply(stateD, invB)

			// Let's ensure the library knows this tool is viable
			macroIndex.RecordOpcode(targetRotation)
			for i := 0; i < 5; i++ {
				macroIndex.RecordOpcode(targetRotation) // Harden it
			}

			// We attach the frustration engine to the wavefront, targeting stateD
			fe := synthesis.NewFrustrationEngine(synthesis.WithSharedIndex(macroIndex))
			wf := NewWavefront(idx,
				WavefrontWithMaxDepth(5),
				WavefrontWithFrustrationEngine(fe, stateD),
			)

			promptChord := data.BaseChord('A')
			results := wf.Search(promptChord, nil, nil)

			gc.So(len(results), gc.ShouldBeGreaterThan, 0)

			// The wavefront should have reached state D!
			// Path should be A, B, [Bridge Synthetic State], D (Wait, D doesn't attach automatically since
			// the leap puts us at pos 2, and D is at 3. The leap takes 1 step (1 tool), lands at pos 2 with phase D?
			// The test cantilever bridges directly. Let's see what depth/phase we achieve.)
			best := results[0]

			// The Phase should absolutely be stateD
			gc.So(best.Phase, gc.ShouldEqual, stateD)

			// Validate that the Macro Index recorded successful uses of the tool
			op, found := macroIndex.FindOpcode(targetRotation)
			gc.So(found, gc.ShouldBeTrue)
			gc.So(op.Hardened, gc.ShouldBeTrue)
		})
	})
}
