package experiment

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestHoldoutExactMeanScorerEnrich(t *testing.T) {
	Convey("Given HoldoutExactMeanScorer and HoldoutScorer", t, func() {
		Convey("It enriches ExperimentalData producing matching Scores and WeightedTotal", func() {
			var exact HoldoutExactMeanScorer
			var hold HoldoutScorer

			dataE := ExperimentalData{
				Holdout:    []byte("abc"),
				Generation: []byte("xabcy"),
			}

			dataH := ExperimentalData{
				Holdout:    []byte("abc"),
				Generation: []byte("xabcy"),
			}

			exact.Enrich(&dataE)
			hold.Enrich(&dataH)

			So(dataE.Scores, ShouldResemble, dataH.Scores)
			So(dataE.WeightedTotal, ShouldEqual, dataH.WeightedTotal)
		})
	})
}

func TestHoldoutExactMeanScorerAggregate(t *testing.T) {
	Convey("HoldoutExactMeanScorer Aggregate means Exact, not WeightedTotal", t, func() {
		var s HoldoutExactMeanScorer

		rows := []ExperimentalData{
			{Scores: Scores{Exact: 0.2, Partial: 1.0, Fuzzy: 1.0}, WeightedTotal: 0.9},
			{Scores: Scores{Exact: 0.4, Partial: 0.0, Fuzzy: 0.0}, WeightedTotal: 0.2},
		}

		So(s.Aggregate(rows), ShouldAlmostEqual, 0.3, 1e-12)
	})
}

func BenchmarkHoldoutExactMeanScorerEnrich(b *testing.B) {
	var s HoldoutExactMeanScorer
	data := ExperimentalData{
		Holdout:    make([]byte, 64),
		Generation: make([]byte, 64),
	}

	for b.Loop() {
		s.Enrich(&data)
	}
}

func TestScalingInstrumentScorerAggregate(t *testing.T) {
	Convey("ScalingInstrumentScorer Aggregate means WeightedTotal", t, func() {
		var s ScalingInstrumentScorer

		rows := []ExperimentalData{
			{WeightedTotal: 1.0},
			{WeightedTotal: 0.5},
		}

		So(s.Aggregate(rows), ShouldAlmostEqual, 0.75, 1e-12)
	})
}
