package algo

import (
	"cmp"
	"slices"

	"github.com/theapemachine/six/pkg/core/numeric"
)

type Algorithm interface {
	numeric.Dynamic
}

/*
Label is a predicted label and its confidence score.
*/
type Label struct {
	Label      []byte
	Confidence float64
}

/*
Continuation is a predicted sequence and its score.
*/
type Continuation struct {
	Sequence []byte
	Score    float64
}

/*
Prediction is a list of predicted labels and continuations.
*/
type Prediction struct {
	Labels        []Label
	Continuations []Continuation
}

/*
NewPrediction creates a new prediction.
*/
func NewPrediction() *Prediction {
	return &Prediction{
		Labels:        make([]Label, 0),
		Continuations: make([]Continuation, 0),
	}
}

/*
String implements the fmt.Stringer interface, and returns
the continuation with the highest score.
*/
func (prediction *Prediction) String() string {
	if len(prediction.Continuations) == 0 {
		return ""
	}

	slices.SortFunc(prediction.Continuations, func(a, b Continuation) int {
		return cmp.Compare(a.Score, b.Score)
	})

	return string(prediction.Continuations[0].Sequence)
}

/*
Label returns the label with the highest confidence score.
*/
func (prediction *Prediction) Label() string {
	if len(prediction.Labels) == 0 {
		return ""
	}

	slices.SortFunc(prediction.Labels, func(a, b Label) int {
		return cmp.Compare(a.Confidence, b.Confidence)
	})

	return string(prediction.Labels[0].Label)
}
