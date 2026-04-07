package surprisal

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestProbabilityUpdate(t *testing.T) {
	t.Parallel()

	Convey("Update no-ops when context shorter than two", t, func() {
		probability := NewProbability()
		prediction := algo.NewPrediction()

		v0, err := primitive.NewValue([]byte("only"))

		So(err, ShouldBeNil)

		defer v0.Close()

		prediction.Context = append(prediction.Context, *v0)

		out, err := probability.Update(prediction)

		So(err, ShouldBeNil)
		So(out.Signals[algo.Surprisal], ShouldEqual, probability.surprisal)
	})

	Convey("Update smooths mean surprisal of the newest slice", t, func() {
		probability := NewProbability()
		prediction := algo.NewPrediction()

		history, err := primitive.NewValue([]byte("common rare"))

		So(err, ShouldBeNil)

		defer history.Close()

		observed, err := primitive.NewValue([]byte("common"))

		So(err, ShouldBeNil)

		defer observed.Close()

		prediction.Context = append(
			prediction.Context,
			*history,
			*observed,
		)

		_, err = probability.Update(prediction)

		So(err, ShouldBeNil)
		So(probability.surprisal.Value(), ShouldBeGreaterThan, 0)
	})
}

func TestProbabilityValue(t *testing.T) {
	t.Parallel()

	Convey("Value returns canonical prediction", t, func() {
		probability := NewProbability()

		So(probability.Value().Signals[algo.Surprisal], ShouldNotBeNil)
	})
}

func BenchmarkProbabilityUpdate(b *testing.B) {
	probability := NewProbability()

	history, err := primitive.NewValue([]byte("aaa bbb ccc ddd"))

	if err != nil {
		b.Fatal(err)
	}

	defer history.Close()

	query, err := primitive.NewValue([]byte("aaa"))

	if err != nil {
		b.Fatal(err)
	}

	defer query.Close()

	prediction := algo.NewPrediction()
	prediction.Context = append(
		prediction.Context,
		*history,
		*query,
	)

	b.ResetTimer()

	for b.Loop() {
		_, _ = probability.Update(prediction)
	}
}
