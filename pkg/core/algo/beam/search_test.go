package beam

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestSearchUpdate(t *testing.T) {
	t.Parallel()

	Convey("Update returns early on empty context", t, func() {
		search := NewSearch()
		prediction := algo.NewPrediction()

		out, err := search.Update(prediction)

		So(err, ShouldBeNil)
		So(out.Continuations, ShouldHaveLength, 0)
	})

	Convey("Update emits continuations when context has tokens", t, func() {
		search := NewSearch()
		prediction := algo.NewPrediction()
		prediction.Labels = append(prediction.Labels, algo.Label{
			Label:      []byte("L"),
			Confidence: 1,
		})

		left, err := primitive.NewValue([]byte("start middle"))

		So(err, ShouldBeNil)

		defer left.Close()

		right, err := primitive.NewValue([]byte("middle end"))

		So(err, ShouldBeNil)

		defer right.Close()

		prediction.Context = append(prediction.Context, *left, *right)

		out, err := search.Update(prediction)

		So(err, ShouldBeNil)
		So(len(out.Continuations), ShouldBeGreaterThanOrEqualTo, 0)
	})
}

func TestSearchValue(t *testing.T) {
	t.Parallel()

	Convey("Value returns internal prediction", t, func() {
		search := NewSearch()

		So(search.Value().Signals[algo.Quality], ShouldNotBeNil)
	})
}

func BenchmarkSearchUpdate(b *testing.B) {
	search := NewSearch()

	left, _ := primitive.NewValue([]byte("alpha beta gamma"))
	right, _ := primitive.NewValue([]byte("beta gamma delta"))

	defer left.Close()
	defer right.Close()

	b.ResetTimer()

	for b.Loop() {
		prediction := algo.NewPrediction()
		prediction.Context = append(
			prediction.Context,
			*left,
			*right,
		)

		_, _ = search.Update(prediction)
	}
}
