package train

import (
	"fmt"
	"math"
	"strings"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/algo/bpe"
	"github.com/theapemachine/six/pkg/core/algo/cooccurrence"
	"github.com/theapemachine/six/pkg/core/algo/episodic"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Online manages label tracking, class totals, and decay for
one training step. It owns the canonical label set and step
counter. The trie composes this as a field and calls Step
for every insert.
*/
type Online struct {
	encoder     *bpe.Encoder
	Labels      []string
	labelSet    map[string]struct{}
	ClassTotals map[string]float64

	/*
		classTotalsLastNormStep records the global CurrentStep at which each
		label's ClassTotals entry was last multiplied through the decay chain.
		Only the label touched in Step is normalized each tick, so inactive
		totals catch up lazily on their next update without scanning every
		label every time.
	*/
	classTotalsLastNormStep map[string]int

	CurrentStep int
	DecayFactor float64

	Episodic       *episodic.Buffer
	Cooccurrence   *cooccurrence.Matrix
	ConceptCounter int
	prediction     *algo.Prediction
}

/*
NewOnline constructs a training driver.
*/
func NewOnline() *Online {
	return &Online{
		encoder:                 bpe.NewEncoder(),
		labelSet:                make(map[string]struct{}),
		ClassTotals:             make(map[string]float64),
		classTotalsLastNormStep: make(map[string]int),
		DecayFactor:             0,
		Cooccurrence:            cooccurrence.NewMatrix(0),
		prediction:              algo.NewPrediction(),
	}
}

func (online *Online) Update(
	prediction *algo.Prediction,
) (*algo.Prediction, error) {
	return prediction, nil
}

func (online *Online) Value() *algo.Prediction {
	return online.prediction
}

/*
Step advances the step counter, registers the label, applies
decay to class totals, and pushes auxiliary signals. Returns
the adjusted decay factor used. The caller handles trie
insertion with the returned step.
*/
func (online *Online) Step(
	label string,
	learningRate float64,
	value primitive.Value,
) {
	label = strings.TrimSpace(label)

	if label == "" || learningRate <= 0 {
		return
	}

	tokens := online.encoder.Encode(value.String())

	online.AddLabel(label)
	online.CurrentStep++

	online.normalize(label, online.CurrentStep)
	online.ClassTotals[label] += learningRate

	if online.Episodic != nil {
		online.Episodic.Push(tokens, label, online.CurrentStep)
	}

	if online.Cooccurrence != nil {
		online.Cooccurrence.Update(tokens)
	}
}

/*
AddLabel registers a label if not already known.
*/
func (online *Online) normalize(label string, step int) {
	lastNormStep, exists := online.classTotalsLastNormStep[label]

	if !exists {
		lastNormStep = 0
	}

	delta := step - lastNormStep

	if delta > 0 {
		online.ClassTotals[label] *= math.Pow(
			online.DecayFactor, float64(delta),
		)
	}

	online.classTotalsLastNormStep[label] = step
}

func (online *Online) AddLabel(label string) {
	if _, exists := online.labelSet[label]; exists {
		return
	}

	online.labelSet[label] = struct{}{}
	online.Labels = append(online.Labels, label)
}

/*
NextConceptLabel generates a new auto-incremented concept label.
*/
func (online *Online) NextConceptLabel() string {
	label := fmt.Sprintf(
		"%s%d",
		core.Cfg.MarkovTrie.ConceptLabelPrefix,
		online.ConceptCounter,
	)

	online.ConceptCounter++

	return label
}
