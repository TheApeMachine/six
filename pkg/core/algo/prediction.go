package algo

import (
	"cmp"
	"slices"

	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/primitive"
)

type Algorithm interface {
	Value() *Prediction
	Update(*Prediction) (*Prediction, error)
}

/*
SignalType identifies a derived signal that an algorithm produces.
Using a typed enum prevents typo-driven bugs and lets the compiler
catch missing cases in switch statements.
*/
type SignalType uint

const (
	Surprisal SignalType = iota
	Entropy
	GrowthRate
	Accuracy
	Quality
)

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
Signals carries the algorithm's derived state — each algorithm
populates the keys it owns (e.g. "surprisal", "entropy",
"growth_rate"). The gossip layer reads these without knowing
which algorithm produced them.
*/
type Prediction struct {
	Labels        []Label
	Continuations []Continuation
	Context       []primitive.Value
	Signals       map[SignalType]*numeric.Derived
}

/*
NewPrediction creates a new prediction.
*/
func NewPrediction() *Prediction {
	return &Prediction{
		Labels:        make([]Label, 0),
		Continuations: make([]Continuation, 0),
		Context:       make([]primitive.Value, 0),
		Signals:       make(map[SignalType]*numeric.Derived),
	}
}

func (prediction *Prediction) Value() *Prediction {
	return prediction
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
		return cmp.Compare(b.Score, a.Score)
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
		return cmp.Compare(b.Confidence, a.Confidence)
	})

	return string(prediction.Labels[0].Label)
}

func (prediction *Prediction) AddLabels(
	labels ...Label,
) *Prediction {
	prediction.Labels = append(
		prediction.Labels, labels...,
	)

	return prediction
}

func (prediction *Prediction) AddContext(
	context ...primitive.Value,
) *Prediction {
	prediction.Context = append(
		prediction.Context, context...,
	)

	return prediction
}
