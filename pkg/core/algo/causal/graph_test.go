package causal

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/primitive"
)

func mustFirstSegment(tb testing.TB, data string) *primitive.Value {
	tb.Helper()

	segmentValue, err := primitive.FirstSegment(primitive.NewValue([]byte(data)))

	if err != nil {
		tb.Fatal(err)
	}

	return segmentValue
}

/*
graphPrediction builds a two-node Prediction using pooled Values. Call
release once the caller is done with the prediction (after Update or when
discarding) so the allocations return to the Value pool.
*/
func graphPrediction(
	label string, fromID, toID uint64, leftText, rightText string,
) (*algo.Prediction, func()) {
	leftChain, err := primitive.NewValue([]byte(leftText))

	if err != nil {
		panic(err)
	}

	rightChain, err := primitive.NewValue([]byte(rightText))

	if err != nil {
		primitive.CloseAll(leftChain)

		panic(err)
	}

	left := leftChain[0]
	right := rightChain[0]

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
		primitive.CloseAll(leftChain)
		primitive.CloseAll(rightChain)
	}

	return prediction, release
}

func TestGraphUpdate(t *testing.T) {
	t.Parallel()

	Convey("Update ignores short context", t, func() {
		graph := NewGraph()
		prediction := algo.NewPrediction()

		segmentValue := mustFirstSegment(t, "solo")

		defer segmentValue.Close()

		prediction.Context = append(prediction.Context, *segmentValue)

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
		So(graph.EdgeReliability(1, 2), ShouldBeGreaterThan, 0)
		So(len(graph.CausalParents(2, 0.1)), ShouldEqual, 1)
	})

	Convey("Update uses captured phase as a causal regime suffix", t, func() {
		graph := NewGraph()

		phase := algo.NewPrediction()
		phase.Signals[algo.GlobalPhase] = numeric.NewDerivedFrom(3)

		_, err := graph.Update(phase)

		So(err, ShouldBeNil)

		pred, release := graphPrediction("env", 11, 12, "p0", "p1")
		_, err = graph.Update(pred)
		release()

		So(err, ShouldBeNil)

		phase = algo.NewPrediction()
		phase.Signals[algo.GlobalPhase] = numeric.NewDerivedFrom(4)

		_, err = graph.Update(phase)

		So(err, ShouldBeNil)

		pred, release = graphPrediction("env", 11, 12, "p0", "p1")
		_, err = graph.Update(pred)
		release()

		So(err, ShouldBeNil)
		So(graph.EdgeInvariance(11, 12), ShouldBeGreaterThan, 0)
	})
}

func TestGraphEdgeInvariance(t *testing.T) {
	t.Parallel()

	Convey("EdgeInvariance is zero for unknown edge", t, func() {
		graph := NewGraph()

		So(graph.EdgeInvariance(9, 9), ShouldEqual, 0)
	})
}

func TestGraphEdgeReliability(t *testing.T) {
	t.Parallel()

	Convey("EdgeReliability keeps single-regime support below transportable invariance", t, func() {
		graph := NewGraph()

		pred, release := graphPrediction("solo", 5, 6, "a", "b")
		_, err := graph.Update(pred)
		release()

		So(err, ShouldBeNil)
		So(graph.EdgeInvariance(5, 6), ShouldEqual, 0)
		So(graph.EdgeReliability(5, 6), ShouldBeGreaterThan, 0)
		So(graph.EdgeReliability(5, 6), ShouldBeLessThan, 0.5)
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

		working := mustFirstSegment(t, "worker")

		defer working.Close()

		forced := mustFirstSegment(t, "forced")

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

func BenchmarkGraphEdgeReliability(b *testing.B) {
	graph := NewGraph()

	pred, release := graphPrediction("a", 3, 4, "l", "r")
	_, _ = graph.Update(pred)
	release()

	b.ResetTimer()

	for b.Loop() {
		_ = graph.EdgeReliability(3, 4)
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

		observed := mustFirstSegment(t, "obs-cf")

		defer observed.Close()

		forced := mustFirstSegment(t, "forced-cf")

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

		predicted := mustFirstSegment(t, "pred-or")

		defer predicted.Close()

		observed := mustFirstSegment(t, "obs-or")

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

		observed := mustFirstSegment(t, "chain")

		defer observed.Close()

		forced := mustFirstSegment(t, "chain-forced")

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

		value := mustFirstSegment(t, "med-v")

		defer value.Close()

		xForced := mustFirstSegment(t, "med-x")

		defer xForced.Close()

		zObs := mustFirstSegment(t, "med-z")

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

		value := mustFirstSegment(t, "mod-v")

		defer value.Close()

		xForced := mustFirstSegment(t, "mod-x")

		defer xForced.Close()

		z1 := mustFirstSegment(t, "mod-z1")

		defer z1.Close()

		z2 := mustFirstSegment(t, "mod-z2")

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

	predicted := mustFirstSegment(b, "bpr")

	defer predicted.Close()

	observed := mustFirstSegment(b, "bor")

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
