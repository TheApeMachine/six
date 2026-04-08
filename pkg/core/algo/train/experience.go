package train

import (
	"fmt"
	"math"
	"strings"
	"sync"

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
fixedSurprisalScaleBits is a numeric.Dynamic that pins the surprisal
denominator to core.Cfg.MarkovTrie.SurprisalScaleBits. Experience uses
it as the default SurprisalScale chain so Run matches the former nil
branch without leaving SurprisalScale unset.
*/
type fixedSurprisalScaleBits struct{}

func (fixed *fixedSurprisalScaleBits) Next(out float64, values ...float64) (float64, error) {
	_ = out
	_ = values

	return core.Cfg.MarkovTrie.SurprisalScaleBits, nil
}

func (fixed *fixedSurprisalScaleBits) Reset() error {
	return nil
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

	SurprisalScale        *numeric.Derived
	UnsupervisedThreshold float64

	/*
		adaptMu plus the fields below implement a lightweight sustained-surprise
		detector: symmetric smoothing is great for noise, but real domain shifts
		show up as several consecutive tokens each far above the running EMA.
		The mutex isolates these scalars because Experience.Run may be invoked
		from pool workers fanning into one trainer.
	*/
	adaptMu            sync.Mutex
	surpriseEMA        float64
	highSurpriseStreak int
	adaptSeeded        bool
}

/*
NewExperience constructs an experience driver. The caller
wires the trie as SurprisalSource, ClassifySource, and
TrainSink. online must be non-nil.
*/
func NewExperience(
	online *Online,
	classifier *classify.Classifier,
	surprisal SurprisalSource,
	scores ClassifySource,
	sink TrainSink,
) (*Experience, error) {
	if online == nil {
		return nil, fmt.Errorf("train: NewExperience requires non-nil online")
	}

	return &Experience{
		Online:     online,
		Classifier: classifier,
		Surprisal:  surprisal,
		Scores:     scores,
		Sink:       sink,
		SurprisalScale: numeric.NewDerived(
			numeric.WithDynamics(&fixedSurprisalScaleBits{}),
		),
		UnsupervisedThreshold: core.Cfg.MarkovTrie.UnsupervisedConfidence,
	}, nil
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

	plasticityBoost := experience.updateSustainedSurpriseBoost(averageBits)

	learningRate := math.Min(
		core.Cfg.MarkovTrie.MaxLearningRate,
		core.Cfg.MarkovTrie.BaselineLearningRate+averageBits/surprisalScale,
	)

	learningRate *= plasticityBoost
	learningRate = math.Min(learningRate, core.Cfg.MarkovTrie.MaxLearningRate)

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

/*
updateSustainedSurpriseBoost tracks an EMA of mean surprisal and bumps
plasticity when several consecutive observations land meaningfully above
that baseline — a cheap distinction between isolated spikes (typos) and
bursts that look like regime change.
*/
func (experience *Experience) updateSustainedSurpriseBoost(averageBits float64) float64 {
	if experience == nil {
		return 1
	}

	alpha := core.Cfg.MarkovTrie.AdaptiveEMAAlpha

	if alpha <= 0 || alpha > 1 {
		alpha = 0.05
	}

	experience.adaptMu.Lock()
	defer experience.adaptMu.Unlock()

	if !experience.adaptSeeded {
		experience.surpriseEMA = averageBits
		experience.adaptSeeded = true

		return 1
	}

	experience.surpriseEMA += alpha * (averageBits - experience.surpriseEMA)

	baseline := experience.surpriseEMA

	if baseline < 1e-9 {
		baseline = 1e-9
	}

	surpriseRatio := averageBits / baseline

	// Fractional gate keeps short-token averages (fractions of a bit) able to
	// register sustained novelty without requiring paragraph-scale surprisal
	// mass every time.
	scaleGate := core.Cfg.MarkovTrie.SurprisalScaleBits * 0.1

	if scaleGate < 1e-6 {
		scaleGate = 1e-6
	}

	if surpriseRatio > 2.0 && averageBits > scaleGate {
		experience.highSurpriseStreak++
	} else {
		experience.highSurpriseStreak = 0
	}

	streak := experience.highSurpriseStreak
	plasticityBoost := 1.0

	const sustainedBursts = 3

	if streak < sustainedBursts {
		return plasticityBoost
	}

	capLen := streak - sustainedBursts + 1

	if capLen > 8 {
		capLen = 8
	}

	plasticityBoost = 1.0 + 0.12*float64(capLen)

	return plasticityBoost
}

func (experience *Experience) assignLabel(sequence string) (string, bool) {
	if experience.Online == nil {
		return core.Cfg.MarkovTrie.ExperienceEmptyLabel, false
	}

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
