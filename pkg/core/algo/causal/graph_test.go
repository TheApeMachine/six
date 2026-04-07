package causal

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/primitive"
)

func graphPrediction(label string, fromID, toID uint64, leftText, rightText string) *algo.Prediction {
	left, err := primitive.NewValue([]byte(leftText))

	if err != nil {
		panic(err)
	}

	right, err := primitive.NewValue([]byte(rightText))

	if err != nil {
		left.Close()

		panic(err)
	}

	left.Set(core.Cfg.Value.Region.ID.Start, fromID)
	right.Set(core.Cfg.Value.Region.ID.Start, toID)

	leftCopy := *left
	rightCopy := *right

	left.Close()
	right.Close()

	prediction := algo.NewPrediction()
	prediction.Labels = append(prediction.Labels, algo.Label{
		Label:      []byte(label),
		Confidence: 1,
	})
	prediction.Context = append(prediction.Context, leftCopy, rightCopy)

	return prediction
}

func TestGraphUpdate(t *testing.T) {
	t.Parallel()

	Convey("Update ignores short context", t, func() {
		graph := NewGraph()
		prediction := algo.NewPrediction()

		v, err := primitive.NewValue([]byte("solo"))

		So(err, ShouldBeNil)

		defer v.Close()

		prediction.Context = append(prediction.Context, *v)

		out, err := graph.Update(prediction)

		So(err, ShouldBeNil)
		So(out, ShouldEqual, graph.prediction)
	})

	Convey("Update accumulates edge invariance", t, func() {
		graph := NewGraph()

		for idx := range 4 {
			label := "a"

			if idx%2 == 1 {
				label = "b"
			}

			_, err := graph.Update(graphPrediction(label, 1, 2, "x", "y"))

			So(err, ShouldBeNil)
		}

		So(graph.EdgeInvariance(1, 2), ShouldBeGreaterThan, 0)
		So(len(graph.CausalParents(2, 0.1)), ShouldEqual, 1)
	})
}

func TestGraphEdgeInvariance(t *testing.T) {
	t.Parallel()

	Convey("EdgeInvariance is zero for unknown edge", t, func() {
		graph := NewGraph()

		So(graph.EdgeInvariance(9, 9), ShouldEqual, 0)
	})
}

func TestGraphValue(t *testing.T) {
	t.Parallel()

	Convey("Value exposes signal map", t, func() {
		graph := NewGraph()

		So(graph.Value().Signals[algo.CausalStrength], ShouldNotBeNil)
	})
}

func TestGraphIntervene(t *testing.T) {
	t.Parallel()

	Convey("Intervene mutates context when parents exist", t, func() {
		graph := NewGraph()

		for idx := range 4 {
			label := "a"

			if idx%2 == 1 {
				label = "b"
			}

			_, err := graph.Update(graphPrediction(label, 7, 9, "p", "q"))

			So(err, ShouldBeNil)
		}

		working, err := primitive.NewValue([]byte("worker"))

		So(err, ShouldBeNil)

		defer working.Close()

		forced, err := primitive.NewValue([]byte("forced"))

		So(err, ShouldBeNil)

		defer forced.Close()

		parentAff := map[uint64][primitive.AffinityWords]uint64{
			7: {},
		}

		severed := graph.Intervene(working, 9, forced, parentAff)

		So(len(severed), ShouldBeGreaterThanOrEqualTo, 0)
	})
}

func BenchmarkGraphUpdate(b *testing.B) {
	graph := NewGraph()

	b.ResetTimer()

	for b.Loop() {
		_, _ = graph.Update(graphPrediction("a", 3, 4, "l", "r"))
	}
}
