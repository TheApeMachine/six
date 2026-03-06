package tokenizer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
)

func TestSequencer(t *testing.T) {
	Convey("Given a new Sequencer and Calibrator", t, func() {
		calibrator := NewCalibrator()
		sequencer := NewSequencer(calibrator)

		Convey("When measure is called on the first byte with a populated chord", func() {
			chord := data.BaseChord('A')
			chord.Set(1)
			chord.Set(2)
			pop, phase := sequencer.measure(0, chord)

			Convey("It should correctly initialize the EMA accumulators", func() {
				So(pop, ShouldBeGreaterThan, 0)
				So(phase, ShouldBeGreaterThanOrEqualTo, 0)
				So(sequencer.emaPop, ShouldEqual, pop)
				So(sequencer.emaPhase, ShouldEqual, phase)
			})
		})

		Convey("When trackVariance is called sequentially", func() {
			chord1 := data.BaseChord('A')
			chord1.Set(1)
			pop1, phase1 := sequencer.measure(0, chord1)
			dPop1, dPhase1 := sequencer.trackVariance(pop1, phase1)

			chord2 := data.BaseChord('B')
			chord2.Set(10)
			chord2.Set(11) // ensures chord2 pop (8+2=10) is different
			pop2, phase2 := sequencer.measure(1, chord2)
			sequencer.trackVariance(pop2, phase2)

			Convey("It should incrementally update the Welford accumulators", func() {
				So(sequencer.count, ShouldEqual, 2)
				So(dPop1, ShouldEqual, 0)
				So(dPhase1, ShouldEqual, 0)
			})

			Convey("It should correctly decay the EWMA state when receiving different inputs", func() {
				// Because EMA alpha is ~0.236, blending requires different values to move
				So(sequencer.emaPop, ShouldNotEqual, pop1)
			})
		})

		Convey("When deriveThresholds is called after some variance", func() {
			chord1 := data.BaseChord('A')
			pop1, phase1 := sequencer.measure(0, chord1)
			sequencer.trackVariance(pop1, phase1)

			chord2 := data.BaseChord('B')
			pop2, phase2 := sequencer.measure(1, chord2)
			sequencer.trackVariance(pop2, phase2)

			pThresh, phThresh := sequencer.deriveThresholds()

			Convey("It should return Z-score thresholds based on standard deviation", func() {
				// We expect at least the mean + precision-level threshold multiplier
				So(pThresh, ShouldBeGreaterThanOrEqualTo, 0)
				So(phThresh, ShouldBeGreaterThanOrEqualTo, 0)
			})
		})

		Convey("When syncBack is called with target values", func() {
			res := sequencer.syncBack(2.0, 1.0)

			Convey("It should ease the current value towards the target based on phiMed", func() {
				// 2.0 * (1 - ~0.055) + 1.0 * ~0.055 = ~1.89 + ~0.055 = ~1.945
				So(res, ShouldBeLessThan, 2.0)
				So(res, ShouldBeGreaterThan, 1.0)
			})
		})
	})
}
