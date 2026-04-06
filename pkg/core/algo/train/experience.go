package train

import (
	"math"
	"strings"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/algo/classify"
	"github.com/theapemachine/six/pkg/core/numeric"
)

/*
SurprisalItem holds one token's information content in bits.
*/
type SurprisalItem struct {
	Token string
	Bits  float64
}

/*
ExperienceResult captures the outcome of one experience step:
which label was assigned, how surprising the input was, and
whether a brand-new concept was spawned.
*/
type ExperienceResult struct {
	Label        string
	Surprisal    float64
	LearningRate float64
	IsNewConcept bool
}

/*
SurprisalSource produces per-token surprisal for a sequence.
The trie implements this — Experience consumes it without
knowing anything about tries.
*/
type SurprisalSource interface {
	SurprisalSeries(sequence string) []SurprisalItem
}

/*
ClassifySource produces per-label scores for a sequence.
Again, the trie implements this.
*/
type ClassifySource interface {
	ClassifyScores(sequence string) map[string]float64
}

/*
TrainSink accepts a training signal for one sequence.
The trie implements this to walk and insert nodes.
*/
type TrainSink interface {
	TrainStep(sequence string, label string, learningRate float64)
}

/*
Experience performs unsupervised or supervised learning with
automatic concept spawning and surprise-modulated learning
rates. It composes a classifier for label inference and an
Online trainer for bookkeeping, and delegates trie-level
work through interfaces.
*/
type Experience struct {
	Online     *Online
	Classifier *classify.Classifier

	Surprisal SurprisalSource
	Scores    ClassifySource
	Sink      TrainSink

	SurprisalScale *numeric.Derived
	UnsupervisedThreshold float64
}

/*
NewExperience constructs an experience driver. The caller
wires the trie as SurprisalSource, ClassifySource, and
TrainSink.
*/
func NewExperience(
	online *Online,
	classifier *classify.Classifier,
	surprisal SurprisalSource,
	scores ClassifySource,
	sink TrainSink,
) *Experience {
	return &Experience{
		Online:                online,
		Classifier:            classifier,
		Surprisal:             surprisal,
		Scores:                scores,
		Sink:                  sink,
		UnsupervisedThreshold: core.Cfg.MarkovTrie.UnsupervisedConfidence,
	}
}

/*
Run performs one experience step. If providedLabel is nil or
empty, unsupervised concept assignment kicks in: existing
labels are scored and the best is picked, or a new concept
is spawned when confidence is below threshold.
*/
func (experience *Experience) Run(
	sequence string,
	providedLabel *string,
) ExperienceResult {
	result := ExperienceResult{
		Label: core.Cfg.MarkovTrie.ExperienceEmptyLabel,
	}

	series := experience.Surprisal.SurprisalSeries(sequence)
	if len(series) == 0 {
		return result
	}

	totalBits := 0.0
	for _, item := range series {
		totalBits += item.Bits
	}

	averageBits := totalBits / float64(len(series))

	surprisalScale := core.Cfg.MarkovTrie.SurprisalScaleBits

	if experience.SurprisalScale != nil {
		scaled, err := experience.SurprisalScale.Next(averageBits)
		
		if err == nil && scaled > 0 {
			surprisalScale = scaled
		}
	}

	learningRate := math.Min(
		core.Cfg.MarkovTrie.MaxLearningRate,
		core.Cfg.MarkovTrie.BaselineLearningRate+averageBits/surprisalScale,
	)

	label := ""
	isNewConcept := false

	if providedLabel != nil {
		label = strings.TrimSpace(*providedLabel)
	}

	if label == "" {
		label, isNewConcept = experience.assignLabel(sequence)
	}

	experience.Sink.TrainStep(sequence, label, learningRate)

	result.Label = label
	result.Surprisal = averageBits
	result.LearningRate = learningRate
	result.IsNewConcept = isNewConcept

	return result
}

func (experience *Experience) assignLabel(sequence string) (string, bool) {
	if len(experience.Online.Labels) == 0 {
		return experience.Online.NextConceptLabel(), true
	}

	scores := experience.Scores.ClassifyScores(sequence)
	bestLabel, maxScore := numeric.ArgmaxStringFloat64(scores)

	if maxScore < experience.UnsupervisedThreshold {
		return experience.Online.NextConceptLabel(), true
	}

	return bestLabel, false
}
