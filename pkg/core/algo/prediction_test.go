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
		prediction := NewPrediction()
		prediction.Continuations = append(prediction.Continuations,
			Continuation{Sequence: []byte("low"), Score: 1})
		prediction.Continuations = append(prediction.Continuations,
			Continuation{Sequence: []byte("high"), Score: 9})

		So(prediction.String(), ShouldEqual, "high")
	})
}

func TestPredictionLabel(t *testing.T) {
	t.Parallel()

	Convey("Label picks highest confidence name", t, func() {
		prediction := NewPrediction()
		prediction.Labels = append(prediction.Labels,
			Label{Label: []byte("a"), Confidence: 0.2})
		prediction.Labels = append(prediction.Labels,
			Label{Label: []byte("b"), Confidence: 0.8})

		So(prediction.Label(), ShouldEqual, "b")
	})
}

func TestPredictionValue(t *testing.T) {
	t.Parallel()

	Convey("Value returns receiver", t, func() {
		prediction := NewPrediction()

		So(prediction.Value(), ShouldEqual, prediction)
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
