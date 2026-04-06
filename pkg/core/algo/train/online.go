package train

import (
	"fmt"
	"strings"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo/cooccurrence"
	"github.com/theapemachine/six/pkg/core/algo/episodic"
)

/*
Online manages label tracking, class totals, and decay for
one training step. It owns the canonical label set and step
counter. The trie composes this as a field and calls Step
for every insert.
*/
type Online struct {
	Labels      []string
	labelSet    map[string]struct{}
	ClassTotals map[string]float64
	CurrentStep int
	DecayFactor float64

	Episodic       *episodic.Buffer
	Cooccurrence   *cooccurrence.Matrix
	ConceptCounter int
}

/*
NewOnline constructs a training driver.
*/
func NewOnline(
	decayFactor float64,
	cooc *cooccurrence.Matrix,
) *Online {
	return &Online{
		labelSet:    make(map[string]struct{}),
		ClassTotals: make(map[string]float64),
		DecayFactor: decayFactor,
		Cooccurrence: cooc,
	}
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
	tokens []string,
	contentTokens []string,
) {
	label = strings.TrimSpace(label)

	if label == "" || learningRate <= 0 {
		return
	}

	online.AddLabel(label)
	online.CurrentStep++

	for _, knownLabel := range online.Labels {
		online.ClassTotals[knownLabel] *= online.DecayFactor
	}

	online.ClassTotals[label] += learningRate

	if online.Episodic != nil {
		online.Episodic.Push(tokens, label, online.CurrentStep)
	}

	if online.Cooccurrence != nil {
		online.Cooccurrence.Update(contentTokens)
	}
}

/*
AddLabel registers a label if not already known.
*/
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
	label := fmt.Sprintf("%s%d", core.Cfg.MarkovTrie.ConceptLabelPrefix, online.ConceptCounter)
	online.ConceptCounter++

	return label
}
