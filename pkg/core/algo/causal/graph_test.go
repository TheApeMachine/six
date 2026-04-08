package causal

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
graphPrediction builds a two-node Prediction using pooled Values. Call
release once the caller is done with the prediction (after Update or when
discarding) so the allocations return to the Value pool.
*/
func graphPrediction(
	label string, fromID, toID uint64, leftText, rightText string,
) (*algo.Prediction, func()) {
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

	prediction := algo.NewPrediction()
	prediction.Targets = append(prediction.Targets, algo.Label{
		Label:      []byte(label),
		Confidence: 1,
	})
	prediction.Context = append(prediction.Context, leftCopy, rightCopy)

	release := func() {
		left.Close()
		right.Close()
	}

	return prediction, release
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

			pred, release := graphPrediction(label, 1, 2, "x", "y")
			_, err := graph.Update(pred)
			release()

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

			pred, release := graphPrediction(label, 7, 9, "p", "q")
			_, err := graph.Update(pred)
			release()

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

		So(severed, ShouldResemble, []uint64{7})
	})
}

func BenchmarkGraphUpdate(b *testing.B) {
	graph := NewGraph()

	b.ResetTimer()

	for b.Loop() {
		pred, release := graphPrediction("a", 3, 4, "l", "r")
		_, _ = graph.Update(pred)
		release()
	}
}

func seedABEdge(graph *Graph, iterations int) {
	for idx := range iterations {
		label := "a"

		if idx%2 == 1 {
			label = "b"
		}

		pred, release := graphPrediction(label, 7, 9, "p", "q")
		_, err := graph.Update(pred)
		release()

		if err != nil {
			panic(err)
		}
	}
}

func TestGraphCounterfactual(t *testing.T) {
	t.Parallel()

	Convey("Counterfactual severs parents and rebinds the forced frame", t, func() {
		graph := NewGraph()

		seedABEdge(graph, 6)

		observed, err := primitive.NewValue([]byte("obs-cf"))

		So(err, ShouldBeNil)

		defer observed.Close()

		forced, err := primitive.NewValue([]byte("forced-cf"))

		So(err, ShouldBeNil)

		defer forced.Close()

		parentAff := map[uint64][primitive.AffinityWords]uint64{
			7: {},
		}

		severed := graph.Counterfactual(observed, 9, forced, parentAff)

		So(severed, ShouldResemble, []uint64{7})
	})
}

func TestGraphObserveResidual(t *testing.T) {
	t.Parallel()

	Convey("ObserveResidual XORs affinity mismatch into the gradient tracker", t, func() {
		graph := NewGraph()

		predicted, err := primitive.NewValue([]byte("pred-or"))

		So(err, ShouldBeNil)

		defer predicted.Close()

		observed, err := primitive.NewValue([]byte("obs-or"))

		So(err, ShouldBeNil)

		defer observed.Close()

		var predAff [primitive.AffinityWords]uint64

		predAff[0] = 0xf00

		var obsAff [primitive.AffinityWords]uint64

		obsAff[0] = 0x00f

		predicted.SetAffinityVector(predAff)
		observed.SetAffinityVector(obsAff)

		tracker := graph.Value().Signals[algo.InterventionResidual]

		So(tracker, ShouldNotBeNil)

		before := tracker.Value()

		graph.ObserveResidual(predicted, observed)

		So(tracker.Value(), ShouldNotEqual, before)
		So(tracker.Value(), ShouldBeGreaterThan, 0)
	})
}

func TestGraphCounterfactualChain(t *testing.T) {
	t.Parallel()

	Convey("CounterfactualChain stops once residuals plateau", t, func() {
		graph := NewGraph()

		seedABEdge(graph, 6)

		observed, err := primitive.NewValue([]byte("chain"))

		So(err, ShouldBeNil)

		defer observed.Close()

		forced, err := primitive.NewValue([]byte("chain-forced"))

		So(err, ShouldBeNil)

		defer forced.Close()

		iv := Intervention{
			Target:           9,
			Forced:           forced,
			ParentAffinities: map[uint64][primitive.AffinityWords]uint64{7: {}},
		}

		predict := func(frame InterventionTarget) InterventionTarget {
			return frame
		}

		results := graph.CounterfactualChain(observed, []Intervention{iv}, predict, 3)

		So(results, ShouldNotBeNil)
		So(len(results), ShouldBeGreaterThan, 0)
		So(results[0].Residual, ShouldEqual, 0)
	})
}

func TestGraphCausalParentsOrder(t *testing.T) {
	t.Parallel()

	Convey("CausalParents sorts by invariance descending", t, func() {
		graph := NewGraph()

		for idx := range 10 {
			label := "a"

			if idx%2 == 1 {
				label = "b"
			}

			pred, release := graphPrediction(label, 100, 300, "s0", "s2")
			_, err := graph.Update(pred)
			release()

			So(err, ShouldBeNil)
		}

		for idx := range 10 {
			label := "b"

			if idx < 9 {
				label = "a"
			}

			pred, release := graphPrediction(label, 200, 300, "s1", "s2")
			_, err := graph.Update(pred)
			release()

			So(err, ShouldBeNil)
		}

		parents := graph.CausalParents(300, 0.01)

		So(len(parents), ShouldEqual, 2)
		So(parents[0].Invariance, ShouldBeGreaterThanOrEqualTo, parents[1].Invariance)
	})
}

func TestGraphMediate(t *testing.T) {
	t.Parallel()

	Convey("Mediate returns zero residuals when prediction is unavailable", t, func() {
		graph := NewGraph()

		seedABEdge(graph, 6)

		value, err := primitive.NewValue([]byte("med-v"))

		So(err, ShouldBeNil)

		defer value.Close()

		xForced, err := primitive.NewValue([]byte("med-x"))

		So(err, ShouldBeNil)

		defer xForced.Close()

		zObs, err := primitive.NewValue([]byte("med-z"))

		So(err, ShouldBeNil)

		defer zObs.Close()

		direct, indirect := graph.Mediate(
			value,
			9,
			xForced,
			9,
			zObs,
			map[uint64][primitive.AffinityWords]uint64{},
			nil,
		)

		So(direct, ShouldEqual, 0)
		So(indirect, ShouldEqual, 0)
	})
}

func TestGraphModerate(t *testing.T) {
	t.Parallel()

	Convey("Moderate returns zero when prediction hook is missing", t, func() {
		graph := NewGraph()

		seedABEdge(graph, 4)

		value, err := primitive.NewValue([]byte("mod-v"))

		So(err, ShouldBeNil)

		defer value.Close()

		xForced, err := primitive.NewValue([]byte("mod-x"))

		So(err, ShouldBeNil)

		defer xForced.Close()

		z1, err := primitive.NewValue([]byte("mod-z1"))

		So(err, ShouldBeNil)

		defer z1.Close()

		z2, err := primitive.NewValue([]byte("mod-z2"))

		So(err, ShouldBeNil)

		defer z2.Close()

		r1, r2 := graph.Moderate(
			value,
			9,
			xForced,
			9,
			z1,
			z2,
			map[uint64][primitive.AffinityWords]uint64{},
			nil,
		)

		So(r1, ShouldEqual, 0)
		So(r2, ShouldEqual, 0)
	})
}

func BenchmarkGraphObserveResidual(b *testing.B) {
	graph := NewGraph()

	predicted, err := primitive.NewValue([]byte("bpr"))

	if err != nil {
		b.Fatal(err)
	}

	defer predicted.Close()

	observed, err := primitive.NewValue([]byte("bor"))

	if err != nil {
		b.Fatal(err)
	}

	defer observed.Close()

	var predAff [primitive.AffinityWords]uint64

	predAff[0] = 0xdead

	var obsAff [primitive.AffinityWords]uint64

	obsAff[0] = 0xbeaf

	predicted.SetAffinityVector(predAff)
	observed.SetAffinityVector(obsAff)

	b.ResetTimer()

	for b.Loop() {
		graph.ObserveResidual(predicted, observed)
	}
}
