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
	CausalStrength
	InterventionResidual
	FieldSurprisal
	FieldGrowth
	FieldDecayMul
	BreakBeam
)

/*
Label is a predicted label and its confidence score.
*/
type Label struct {
	Label      []byte
	Confidence float64
}

/*
Continuation is a predicted sequence and its score. Origin identifies
which trie produced it so the node-level beam can trace selections
back to their source without extra bookkeeping.
*/
type Continuation struct {
	Sequence []byte
	Score    float64
	Origin   uint64
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
	Rejected      []uint64
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

/*
TruncateForUpdate clears Labels, Continuations, and Context so a
Prediction may be reused as a disposable payload for Algorithm.Update.
Signals are left intact — use this only on observers allocated for
that purpose (e.g. NewPrediction in the caller), never on an
algorithm's canonical Value().
*/
func (prediction *Prediction) TruncateForUpdate() {
	prediction.Labels = prediction.Labels[:0]
	prediction.Continuations = prediction.Continuations[:0]
	prediction.Context = prediction.Context[:0]
}

/*
Clone returns a deep copy of the Prediction so callers can merge or
annotate results without mutating an algorithm's canonical Value.
*/
func (prediction *Prediction) Clone() *Prediction {
	if prediction == nil {
		return nil
	}

	return NewPrediction().Merge(prediction)
}

/*
Merge appends Labels, Continuations, Context, and Rejected entries from
other into prediction. Signals are cloned and assigned by key, so later
merges overwrite earlier values for the same signal type.
*/
func (prediction *Prediction) Merge(other *Prediction) *Prediction {
	if prediction == nil {
		return nil
	}

	if other == nil {
		return prediction
	}

	for _, label := range other.Labels {
		prediction.Labels = append(prediction.Labels, Label{
			Label:      append([]byte(nil), label.Label...),
			Confidence: label.Confidence,
		})
	}

	for _, continuation := range other.Continuations {
		prediction.Continuations = append(prediction.Continuations, Continuation{
			Sequence: append([]byte(nil), continuation.Sequence...),
			Score:    continuation.Score,
			Origin:   continuation.Origin,
		})
	}

	prediction.Context = append(prediction.Context, other.Context...)
	prediction.Rejected = append(prediction.Rejected, other.Rejected...)

	for signalType, signal := range other.Signals {
		if signal == nil {
			continue
		}

		prediction.Signals[signalType] = signal.Clone()
	}

	return prediction
}

/*
SetContinuationOrigin stamps origin onto continuations that do not
already identify their source. This lets a caller annotate merged beam
results without knowing which algorithm produced them.
*/
func (prediction *Prediction) SetContinuationOrigin(origin uint64) *Prediction {
	if prediction == nil {
		return nil
	}

	if origin == 0 {
		return prediction
	}

	for idx := range prediction.Continuations {
		if prediction.Continuations[idx].Origin != 0 {
			continue
		}

		prediction.Continuations[idx].Origin = origin
	}

	return prediction
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

	slices.SortStableFunc(prediction.Continuations, func(a, b Continuation) int {
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

	slices.SortStableFunc(prediction.Labels, func(a, b Label) int {
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
