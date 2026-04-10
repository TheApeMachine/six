package markovtrie

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestGeometricContinuationBoost(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("Given a geometric context and attractor", t, func() {
		current := primitive.FrameMultivector{1}
		attractor := primitive.FrameMultivector{1}
		aligned := primitive.FrameMultivector{1}
		opposed := primitive.FrameMultivector{-1}

		Convey("It should boost aligned candidates more than opposed candidates", func() {
			alignedBoost := geometricContinuationBoost(current, aligned, attractor)
			opposedBoost := geometricContinuationBoost(current, opposed, attractor)

			So(alignedBoost, ShouldBeGreaterThan, 0)
			So(opposedBoost, ShouldEqual, 0)
		})
	})
}

func TestStoreRescoreGeometricContinuations(t *testing.T) {
	setupMarkovTrieValueConfig(t)

	t.Parallel()

	Convey("Given continuations and a geometric context", t, func() {
		store, err := NewStore(t.Context(), primitive.Affinity{})

		So(err, ShouldBeNil)

		value := primitive.Value{}
		value.SetContextMultivector(primitive.NewFrameMultivector([]byte("alpha")))

		prediction := algo.NewPrediction()
		prediction.AddContext(value)
		prediction.Continuations = append(
			prediction.Continuations,
			algo.Continuation{Sequence: []byte("alpha"), Score: 0},
		)

		store.rescoreGeometricContinuations(prediction)

		So(prediction.Continuations[0].Score, ShouldBeGreaterThan, 0)
	})
}
