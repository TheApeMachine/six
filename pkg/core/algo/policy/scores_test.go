package policy

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewActionScores(t *testing.T) {
	t.Parallel()

	Convey("NewActionScores lifts score and support maps", t, func() {
		scores := NewActionScores(
			map[string]float64{
				"left":  2,
				"right": 1,
			},
			map[string]float64{
				"left":  3,
				"right": 1,
			},
		)

		So(len(scores), ShouldEqual, 2)
	})
}

func TestActionScoresSortDescending(t *testing.T) {
	t.Parallel()

	Convey("SortDescending orders by score, then support, then action", t, func() {
		scores := ActionScores{
			{Action: "b", Score: 1, Support: 1},
			{Action: "a", Score: 1, Support: 2},
			{Action: "c", Score: 3, Support: 1},
		}

		scores.SortDescending()

		So(scores[0].Action, ShouldEqual, "c")
		So(scores[1].Action, ShouldEqual, "a")
		So(scores[2].Action, ShouldEqual, "b")
	})
}

func TestActionScoresPrediction(t *testing.T) {
	t.Parallel()

	Convey("Prediction projects actions into continuations", t, func() {
		prediction := ActionScores{
			{Action: "left", Score: 2},
			{Action: "right", Score: 1},
		}.Prediction()

		So(prediction, ShouldNotBeNil)
		So(len(prediction.Continuations), ShouldEqual, 2)
		So(string(prediction.Continuations[0].Sequence), ShouldEqual, "left")
	})
}

func BenchmarkActionScoresSortDescending(b *testing.B) {
	template := ActionScores{
		{Action: "left", Score: 2, Support: 3},
		{Action: "right", Score: 1, Support: 2},
		{Action: "forward", Score: 4, Support: 1},
	}

	b.ResetTimer()

	for b.Loop() {
		scores := append(ActionScores(nil), template...)
		scores.SortDescending()
	}
}
