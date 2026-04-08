package algo

import (
	"errors"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core/numeric"
)

type stubAlgorithm struct {
	prediction *Prediction
	err        error
	updates    int
}

func (stub *stubAlgorithm) Value() *Prediction {
	if stub == nil {
		return nil
	}

	return stub.prediction
}

func (stub *stubAlgorithm) Update(prediction *Prediction) (*Prediction, error) {
	if stub == nil {
		return nil, nil
	}

	stub.updates++

	return stub.prediction, stub.err
}

func TestNewStack(t *testing.T) {
	t.Parallel()

	Convey("NewStack preserves configured order", t, func() {
		first := &stubAlgorithm{}
		second := &stubAlgorithm{}

		stack := NewStack(first, second)

		So(len(stack.Algorithms()), ShouldEqual, 2)
		So(stack.Algorithms()[0], ShouldEqual, first)
		So(stack.Algorithms()[1], ShouldEqual, second)
	})
}

func TestStackUpdate(t *testing.T) {
	t.Parallel()

	Convey("Update runs every algorithm, merges values, and joins errors", t, func() {
		firstPrediction := NewPrediction()
		firstPrediction.Labels = append(firstPrediction.Labels, Label{
			Label:      []byte("left"),
			Confidence: 0.6,
		})
		firstPrediction.Signals[Surprisal] = numeric.NewDerivedFrom(0.4)

		secondPrediction := NewPrediction()
		secondPrediction.Continuations = append(secondPrediction.Continuations, Continuation{
			Sequence: []byte("right"),
			Score:    2,
		})

		first := &stubAlgorithm{
			prediction: firstPrediction,
			err:        errors.New("first"),
		}
		second := &stubAlgorithm{
			prediction: secondPrediction,
		}

		stack := NewStack(first, second)
		out, err := stack.Update(NewPrediction())

		So(err, ShouldNotBeNil)
		So(first.updates, ShouldEqual, 1)
		So(second.updates, ShouldEqual, 1)
		So(len(out.Labels), ShouldEqual, 1)
		So(len(out.Continuations), ShouldEqual, 1)
		So(out.Signals[Surprisal].Value(), ShouldAlmostEqual, 0.4, 1e-9)
	})
}

func TestStackValue(t *testing.T) {
	t.Parallel()

	Convey("Value merges the stack without mutating source predictions", t, func() {
		prediction := NewPrediction()
		prediction.Continuations = append(prediction.Continuations, Continuation{
			Sequence: []byte("beam"),
			Score:    1,
		})

		stack := NewStack(&stubAlgorithm{
			prediction: prediction,
		})

		out := stack.Value()
		out.Continuations[0].Sequence[0] = 't'

		So(string(prediction.Continuations[0].Sequence), ShouldEqual, "beam")
	})
}

func TestStackSignals(t *testing.T) {
	t.Parallel()

	Convey("Signals and Signal flatten current algorithm outputs", t, func() {
		left := NewPrediction()
		right := NewPrediction()
		left.Signals[Entropy] = numeric.NewDerivedFrom(0.2)
		right.Signals[Quality] = numeric.NewDerivedFrom(0.9)

		stack := NewStack(
			&stubAlgorithm{prediction: left},
			&stubAlgorithm{prediction: right},
		)

		signals := stack.Signals()

		So(signals[Entropy], ShouldAlmostEqual, 0.2, 1e-9)
		So(signals[Quality], ShouldAlmostEqual, 0.9, 1e-9)
		So(stack.Signal(Quality), ShouldAlmostEqual, 0.9, 1e-9)
	})
}

func BenchmarkStackUpdate(b *testing.B) {
	left := NewPrediction()
	left.Labels = append(left.Labels, Label{
		Label:      []byte("bench"),
		Confidence: 1,
	})

	right := NewPrediction()
	right.Continuations = append(right.Continuations, Continuation{
		Sequence: []byte("beam"),
		Score:    1,
	})

	stack := NewStack(
		&stubAlgorithm{prediction: left},
		&stubAlgorithm{prediction: right},
	)

	b.ResetTimer()

	for b.Loop() {
		_, err := stack.Update(NewPrediction())

		if err != nil {
			b.Fatal(err)
		}
	}
}
