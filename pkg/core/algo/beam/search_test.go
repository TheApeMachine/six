package beam

import (
	"math"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/core/numeric/gf"
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
		So(len(out.Continuations), ShouldBeGreaterThan, 0)
	})
}

func TestSearchValue(t *testing.T) {
	t.Parallel()

	Convey("Value returns internal prediction", t, func() {
		search := NewSearch()

		So(search.Value().Signals[algo.Quality], ShouldNotBeNil)
	})
}

func TestSearchPhaseBias(t *testing.T) {
	t.Parallel()

	Convey("Global phase bias boosts aligned continuations", t, func() {
		baselineSearch := NewSearch()

		continuations := []algo.Continuation{
			{Sequence: []byte("alpha"), Score: 0},
			{Sequence: []byte("omega"), Score: 0},
		}

		baselinePrediction := algo.NewPrediction()
		baselinePrediction.Continuations = append(
			baselinePrediction.Continuations,
			continuations...,
		)

		baselineRanked := baselineSearch.rankFromContinuations(baselinePrediction)

		So(baselineRanked, ShouldHaveLength, 2)
		So(baselineRanked[0].Probability, ShouldEqual, baselineRanked[1].Probability)

		search := NewSearch()
		alignedPhase := gf.DominantForBytes([]byte("alpha"))
		phaseSignal := algo.NewPrediction()
		phaseSignal.Signals[algo.GlobalPhase] = numeric.NewDerivedFrom(float64(alignedPhase.Index))
		phaseSignal.Signals[algo.PhaseConcentration] = numeric.NewDerivedFrom(1)

		_, err := search.Update(phaseSignal)

		So(err, ShouldBeNil)

		prediction := algo.NewPrediction()
		prediction.Continuations = append(prediction.Continuations, continuations...)

		ranked := search.rankFromContinuations(prediction)

		So(ranked, ShouldHaveLength, 2)
		So(ranked[0].Token, ShouldEqual, "alpha")
		So(ranked[0].Probability, ShouldBeGreaterThan, ranked[1].Probability)
	})

	Convey("non-finite GlobalPhase leaves continuations unbiased", t, func() {
		search := NewSearch()
		phaseSignal := algo.NewPrediction()
		phaseSignal.Signals[algo.GlobalPhase] = numeric.NewDerivedFrom(math.NaN())
		phaseSignal.Signals[algo.PhaseConcentration] = numeric.NewDerivedFrom(1)

		_, err := search.Update(phaseSignal)

		So(err, ShouldBeNil)

		prediction := algo.NewPrediction()
		prediction.Continuations = append(
			prediction.Continuations,
			algo.Continuation{Sequence: []byte("alpha"), Score: 0},
			algo.Continuation{Sequence: []byte("omega"), Score: 0},
		)

		ranked := search.rankFromContinuations(prediction)

		So(ranked, ShouldHaveLength, 2)
		So(ranked[0].Probability, ShouldEqual, ranked[1].Probability)
	})
}

func BenchmarkSearchUpdate(b *testing.B) {
	search := NewSearch()

	left, err := primitive.NewValue([]byte("alpha beta gamma"))

	if err != nil {
		b.Fatal(err)
	}

	defer left.Close()

	right, err := primitive.NewValue([]byte("beta gamma delta"))

	if err != nil {
		b.Fatal(err)
	}

	defer right.Close()

	b.ResetTimer()

	for b.Loop() {
		prediction := algo.NewPrediction()
		prediction.Context = append(
			prediction.Context,
			*left,
			*right,
		)

		_, err := search.Update(prediction)

		if err != nil {
			b.Fatal(err)
		}
	}
}
