package infer

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestRun(t *testing.T) {
	Convey("Run should short-circuit when ShouldSkip", t, func() {
		out := Run("x", Env{
			ShouldSkip: func(string) bool { return true },
		})

		So(out.Label, ShouldBeEmpty)
		So(out.Scores, ShouldBeNil)
	})

	Convey("Run should merge distinct sample into continuations", t, func() {
		out := Run("data", Env{
			ShouldSkip: func(string) bool {
				return false
			},
			MeanSurprisalBits: func(string) float64 {
				return 0
			},
			SurprisalGate: 100,
			Classify: func(string) map[string]float64 {
				return map[string]float64{"L": 50}
			},
			BestLabel: func(m map[string]float64) (string, float64) {
				return "L", m["L"]
			},
			InferenceParams: nil,
			BeamSearch: func(string, string, int, int) []Continuation {
				return nil
			},
			Generate: func(string, string, float64, int) string {
				return "extra"
			},
		})

		So(len(out.Continuations), ShouldEqual, 1)
		So(out.Continuations[0].Sequence, ShouldEqual, "extra")
	})
}

func TestAppendIfNewSequence(t *testing.T) {
	Convey("appendIfNewSequence skips duplicates", t, func() {
		base := []Continuation{{Sequence: "a", Score: 1}}
		next := appendIfNewSequence(base, "a", 0)

		So(len(next), ShouldEqual, 1)
	})
}
