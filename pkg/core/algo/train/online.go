package train

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/algo/bpe"
	"github.com/theapemachine/six/pkg/core/algo/cooccurrence"
	"github.com/theapemachine/six/pkg/core/algo/episodic"
	"github.com/theapemachine/six/pkg/core/numeric"
	"github.com/theapemachine/six/pkg/core/numeric/adaptive"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Online manages label tracking, class totals, and decay for
one training step. It owns the canonical label set, step
counter, and all derived signals that emerge from training
dynamics.

Signals produced:
  - Surprisal: smoothed mean surprisal of incoming observations
  - GrowthRate: rate of change of the node count (derived from
    step count delta, not a magic constant)
*/
type Online struct {
	/*
		mu serializes map mutations and counters. markovtrie.Store.Load may be
		invoked from many pool workers against one cluster, so trainer state must
		not allow concurrent writers on labelSet, ClassTotals, or
		classTotalsLastNormStep.
	*/
	mu          sync.Mutex
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

	surprisal  *numeric.Derived
	growthRate *numeric.Derived

	/*
		plasticity derives the learning rate from surprisal without magic
		numbers. The Ratio dynamic divides surprisal by its own smoothed
		mean, producing a dimensionless multiplier: 1.0 when surprisal
		equals its average, >1 for novel input, <1 for expected input.
		The Inverse provides a floor that tracks the observed range.
	*/
	plasticity *numeric.Derived
	lastRate   float64
}

/*
NewOnline constructs a training driver with self-tuning signal chains.
*/
func NewOnline() *Online {
	surprisal := numeric.NewDerived(
		numeric.WithDynamics(adaptive.NewEMA()),
	)

	growthRate := numeric.NewDerived(
		numeric.WithDynamics(adaptive.NewDelta(0), adaptive.NewEMA()),
	)

	plasticity := numeric.NewDerived(
		numeric.WithDynamics(adaptive.NewEMA()),
	)

	prediction := algo.NewPrediction()
	prediction.Signals[algo.Surprisal] = surprisal
	prediction.Signals[algo.GrowthRate] = growthRate

	return &Online{
		encoder:                 bpe.NewEncoder(),
		labelSet:                make(map[string]struct{}),
		ClassTotals:             make(map[string]float64),
		classTotalsLastNormStep: make(map[string]int),
		DecayFactor:             0,
		Cooccurrence:            cooccurrence.NewMatrix(0),
		prediction:              prediction,
		surprisal:               surprisal,
		growthRate:              growthRate,
		plasticity:              plasticity,
		lastRate:                1.0,
	}
}

/*
Update receives an observation from the Store orchestrator. The
Prediction carries target labels (what to train) and context (the values).
MeanSurprisal is set by the Store from its trie walk.

The trainer pushes the surprisal observation through its Derived
chain and derives the learning rate from the chain's output. No
magic constants — plasticity emerges from the ratio of current
surprisal to smoothed surprisal.
*/
func (online *Online) Update(
	prediction *algo.Prediction,
) (*algo.Prediction, error) {
	if prediction == nil {
		return online.prediction, nil
	}

	targets := prediction.SupervisionLabels()
	var novelty float64

	if len(targets) > 0 && len(prediction.Context) > 0 {
		novelty = online.contextNovelty(prediction)
	}

	online.mu.Lock()
	defer online.mu.Unlock()

	online.applyFieldPressure(prediction)

	if len(targets) == 0 {
		return online.prediction, nil
	}

	label := string(targets[0].Label)

	online.surprisal.Next(novelty)
	online.growthRate.Next(float64(online.CurrentStep))

	smoothedSurprisal := online.surprisal.Value()

	if smoothedSurprisal > 0 {
		online.plasticity.Next(novelty)
		plasticityValue := online.plasticity.Value()

		if plasticityValue > 0 {
			online.lastRate = math.Min(1.0, plasticityValue/smoothedSurprisal)
		}
	}

	for _, value := range prediction.Context {
		online.stepUnlocked(label, online.lastRate, value)
	}

	return online.prediction, nil
}

func (online *Online) Value() *algo.Prediction {
	online.mu.Lock()
	defer online.mu.Unlock()

	return online.prediction
}

/*
LearningRate returns the current derived learning rate. The Store
reads this after Update to pass into the trie insertion.
*/
func (online *Online) LearningRate() float64 {
	online.mu.Lock()
	defer online.mu.Unlock()

	return online.lastRate
}

/*
applyFieldPressure checks for field pressure signals in the incoming
Prediction and modulates decay and learning rate accordingly. Each
algorithm owns its own response to pressure — the Store just
broadcasts.
*/
func (online *Online) applyFieldPressure(prediction *algo.Prediction) {
	if prediction.Signals == nil {
		return
	}

	if decayMul, ok := prediction.Signals[algo.FieldDecayMul]; ok {
		mul := decayMul.Value()

		if mul != 0 {
			online.DecayFactor = math.Max(0, math.Min(1,
				core.Cfg.MarkovTrie.DecayFactor*mul,
			))
		}
	}

	if growth, ok := prediction.Signals[algo.FieldGrowth]; ok {
		fieldGrowth := growth.Value()
		pressureMul := 1.0

		if fieldGrowth > 0 {
			pressureMul = 1.0 + fieldGrowth
		} else {
			pressureMul = 1.0 / (1.0 - fieldGrowth)
		}

		online.lastRate = math.Min(1.0, math.Max(0, online.lastRate*pressureMul))
	}
}

/*
Step advances the step counter, registers the label, applies
decay to class totals, and pushes auxiliary signals.
*/
func (online *Online) Step(
	label string,
	learningRate float64,
	value primitive.Value,
) {
	online.mu.Lock()
	defer online.mu.Unlock()

	online.stepUnlocked(label, learningRate, value)
}

func (online *Online) stepUnlocked(
	label string,
	learningRate float64,
	value primitive.Value,
) {
	label = strings.TrimSpace(label)

	if label == "" || learningRate <= 0 {
		return
	}

	tokens := online.encoder.EncodeBytes(value.TokenRegionBytes())

	online.addLabelUnlocked(label)
	online.CurrentStep++

	online.normalizeUnlocked(label, online.CurrentStep)
	online.ClassTotals[label] += learningRate

	if online.Episodic != nil {
		online.Episodic.Push(tokens, label, online.CurrentStep)
	}

	if online.Cooccurrence != nil {
		online.Cooccurrence.Update(tokens)
	}
}

func (online *Online) normalizeUnlocked(label string, step int) {
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
	online.mu.Lock()
	defer online.mu.Unlock()

	online.addLabelUnlocked(label)
}

func (online *Online) addLabelUnlocked(label string) {
	if _, exists := online.labelSet[label]; exists {
		return
	}

	online.labelSet[label] = struct{}{}
	online.Labels = append(online.Labels, label)
}

/*
contextNovelty measures how surprising the incoming context is
relative to previously seen tokens. Builds a frequency table from
Context values and returns the Shannon entropy in bits — high
entropy means diverse/novel input, low means repetitive/expected.
*/
func (online *Online) contextNovelty(prediction *algo.Prediction) float64 {
	freq := make(map[string]float64)
	var total float64

	for _, value := range prediction.Context {
		slab := value.TokenRegionBytes()

		for _, field := range bytes.Fields(slab) {
			token := string(field)
			freq[token]++
			total++
		}
	}

	if total == 0 {
		return 0
	}

	var bits float64

	for _, n := range freq {
		prob := n / total
		bits -= prob * math.Log2(prob)
	}

	return bits
}

/*
NextConceptLabel generates a new auto-incremented concept label.
*/
func (online *Online) NextConceptLabel() string {
	online.mu.Lock()
	defer online.mu.Unlock()

	label := fmt.Sprintf(
		"%s%d",
		core.Cfg.MarkovTrie.ConceptLabelPrefix,
		online.ConceptCounter,
	)

	online.ConceptCounter++

	return label
}
