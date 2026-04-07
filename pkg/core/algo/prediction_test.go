package algo

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestTruncateForUpdate(t *testing.T) {
	Convey("Given a Prediction reused as an Update carrier", t, func() {
		prediction := NewPrediction()
		prediction.Signals[Surprisal] = nil

		var stub primitive.Value
		stub[0] = 1

		prediction.AddLabels(Label{Label: []byte("a"), Confidence: 1})
		prediction.AddContext(stub)

		Convey("When TruncateForUpdate runs", func() {
			prediction.TruncateForUpdate()

			Convey("It should clear labels and context but keep the Signals map", func() {
				So(prediction.Labels, ShouldHaveLength, 0)
				So(prediction.Context, ShouldHaveLength, 0)
				So(prediction.Continuations, ShouldHaveLength, 0)
				So(prediction.Signals, ShouldContainKey, Surprisal)
			})
		})
	})
}

func BenchmarkTruncateForUpdate(b *testing.B) {
	prediction := NewPrediction()

	for range b.N {
		prediction.Labels = append(prediction.Labels[:0], Label{Label: []byte("x")})
		prediction.Context = append(prediction.Context[:0], primitive.Value{})
		prediction.TruncateForUpdate()
	}
}

func TestPredictionString(t *testing.T) {
	t.Parallel()

	Convey("String picks highest scoring continuation text", t, func() {
		Convey("when Continuations is empty, It should return an empty string", func() {
			prediction := NewPrediction()

			So(prediction.String(), ShouldEqual, "")
		})

		Convey("when scores differ, It should pick the max", func() {
			prediction := NewPrediction()
			prediction.Continuations = append(prediction.Continuations,
				Continuation{Sequence: []byte("low"), Score: 1})
			prediction.Continuations = append(prediction.Continuations,
				Continuation{Sequence: []byte("high"), Score: 9})

			So(prediction.String(), ShouldEqual, "high")
		})

		Convey("when top scores tie, It should keep earlier slice order", func() {
			prediction := NewPrediction()
			prediction.Continuations = append(prediction.Continuations,
				Continuation{Sequence: []byte("first"), Score: 3})
			prediction.Continuations = append(prediction.Continuations,
				Continuation{Sequence: []byte("second"), Score: 3})

			So(prediction.String(), ShouldEqual, "first")
		})

		Convey("when all scores are non-positive, It should still pick the highest", func() {
			prediction := NewPrediction()
			prediction.Continuations = append(prediction.Continuations,
				Continuation{Sequence: []byte("worst"), Score: -9})
			prediction.Continuations = append(prediction.Continuations,
				Continuation{Sequence: []byte("less_bad"), Score: -2})

			So(prediction.String(), ShouldEqual, "less_bad")
		})

		Convey("when a zero score beats a negative one", func() {
			prediction := NewPrediction()
			prediction.Continuations = append(prediction.Continuations,
				Continuation{Sequence: []byte("neg"), Score: -1})
			prediction.Continuations = append(prediction.Continuations,
				Continuation{Sequence: []byte("zero"), Score: 0})

			So(prediction.String(), ShouldEqual, "zero")
		})
	})
}

func TestPredictionLabel(t *testing.T) {
	t.Parallel()

	Convey("Label picks highest confidence name", t, func() {
		Convey("when Labels is empty, It should return an empty string", func() {
			prediction := NewPrediction()

			So(prediction.Label(), ShouldEqual, "")
		})

		Convey("when confidences differ inside [0,1], It should pick the max", func() {
			prediction := NewPrediction()
			prediction.Labels = append(prediction.Labels,
				Label{Label: []byte("a"), Confidence: 0.2})
			prediction.Labels = append(prediction.Labels,
				Label{Label: []byte("b"), Confidence: 0.8})

			So(prediction.Label(), ShouldEqual, "b")
		})

		Convey("when confidences tie, It should keep earlier slice order", func() {
			prediction := NewPrediction()
			prediction.Labels = append(prediction.Labels,
				Label{Label: []byte("first"), Confidence: 0.5})
			prediction.Labels = append(prediction.Labels,
				Label{Label: []byte("second"), Confidence: 0.5})

			So(prediction.Label(), ShouldEqual, "first")
		})

		Convey("when confidences sit outside [0,1], It still ranks by numeric order", func() {
			prediction := NewPrediction()
			prediction.Labels = append(prediction.Labels,
				Label{Label: []byte("low"), Confidence: -0.25})
			prediction.Labels = append(prediction.Labels,
				Label{Label: []byte("high"), Confidence: 1.25})

			So(prediction.Label(), ShouldEqual, "high")

			prediction.Labels = prediction.Labels[:0]
			prediction.Labels = append(prediction.Labels,
				Label{Label: []byte("a"), Confidence: 2},
				Label{Label: []byte("b"), Confidence: 1.5},
			)

			So(prediction.Label(), ShouldEqual, "a")
		})
	})
}

func TestPredictionAddLabelsAddContext(t *testing.T) {
	t.Parallel()

	Convey("AddLabels and AddContext chain", t, func() {
		prediction := NewPrediction()
		var stub primitive.Value

		stub[0] = 3

		updated := prediction.AddLabels(Label{Label: []byte("z"), Confidence: 1}).
			AddContext(stub)

		So(updated, ShouldEqual, prediction)
		So(len(updated.Labels), ShouldEqual, 1)
		So(len(updated.Context), ShouldEqual, 1)
	})
}

func BenchmarkPredictionString(b *testing.B) {
	prediction := NewPrediction()

	for idx := range 16 {
		prediction.Continuations = append(prediction.Continuations, Continuation{
			Sequence: []byte{'a' + byte(idx)},
			Score:    float64(idx),
		})
	}

	b.ResetTimer()

	for b.Loop() {
		_ = prediction.String()
	}
}
