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
