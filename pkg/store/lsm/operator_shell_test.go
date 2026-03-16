package lsm

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

func TestPredictNextPhasePrefersTrajectorySnapshot(t *testing.T) {
	gc.Convey("Given a value with both an affine operator and an explicit trajectory snapshot", t, func() {
		calc := numeric.NewCalculus()
		value := data.NeutralValue()
		value.SetAffine(7, 5)
		value.SetTrajectory(19, 42)
		value.SetGuardRadius(2)

		gc.Convey("predictNextPhaseFromValue should prefer the stored trajectory when the current phase matches", func() {
			got := predictNextPhaseFromValue(calc, value, 19, 'x')
			gc.So(got, gc.ShouldEqual, numeric.Phase(42))
		})
	})
}

func TestOperatorPhaseAcceptanceUsesGuardRadius(t *testing.T) {
	gc.Convey("Given a guarded operator shell", t, func() {
		value := data.NeutralValue()
		value.SetGuardRadius(2)

		gc.Convey("nearby drift should be accepted with a penalty", func() {
			accepted, penalty, ok := operatorPhaseAcceptance(value, 10, 12)
			gc.So(ok, gc.ShouldBeTrue)
			gc.So(accepted, gc.ShouldEqual, numeric.Phase(12))
			gc.So(penalty, gc.ShouldEqual, 2)
		})

		gc.Convey("larger drift should still be rejected", func() {
			_, _, ok := operatorPhaseAcceptance(value, 10, 14)
			gc.So(ok, gc.ShouldBeFalse)
		})
	})
}

func TestWavefrontGuardAllowsNearPhaseContinuation(t *testing.T) {
	gc.Convey("Given a guarded operator whose next stored phase drifts slightly", t, func() {
		idx := NewSpatialIndexServer()
		calc := numeric.NewCalculus()

		aPhase := calc.Multiply(1, calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32('a')))
		expectedBPhase := calc.Multiply(aPhase, calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32('b')))
		driftedBPhase := numeric.Phase((uint32(expectedBPhase) + 1) % numeric.FermatPrime)
		if driftedBPhase == 0 {
			driftedBPhase = 1
		}

		aValue := observableValue('a', aPhase, data.OpcodeNext, 'b')
		aValue.SetGuardRadius(2)
		bValue := observableValue('b', driftedBPhase, data.OpcodeHalt, 0)

		idx.insertSync(morton.Pack(0, 'a'), aValue, data.MustNewValue())
		idx.insertSync(morton.Pack(1, 'b'), bValue, data.MustNewValue())

		wf := NewWavefront(idx, WavefrontWithMaxHeads(16), WavefrontWithMaxDepth(4), WavefrontWithMaxFuzzy(0))

		gc.Convey("SearchPrompt should still decode the prompt path", func() {
			results := wf.SearchPrompt([]byte("ab"), nil, nil)
			gc.So(len(results), gc.ShouldBeGreaterThan, 0)

			decoded := idx.decodeValues(results[0].Path)
			gc.So(len(decoded), gc.ShouldBeGreaterThan, 0)
			gc.So(string(decoded[0]), gc.ShouldEqual, "ab")
		})
	})
}
