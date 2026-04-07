package replay

import (
	"unicode/utf8"
)

/*
Acceptance is a replay step that passed confidence and novelty gates.
*/
type Acceptance struct {
	Sequence   string
	Label      string
	Confidence float64
}

/*
Env binds replay to a concrete store or simulator without importing markovtrie.
*/
type Env struct {
	PickLabel            func() string
	GenerateContinuation func(label string, temperature float64, maxLen int) string
	LabelConfidence      func(sequence string, label string) float64
	PathIsNovel          func(sequence string) bool
	Train                func(sequence string, label string, learningRate float64)

	/*
		LearningRate is passed to Train on successful acceptance. Zero means
		use 1.0 so existing callers stay byte-for-byte equivalent unless they
		set this field.
	*/
	LearningRate float64
}

/*
TryOnce runs one replay trial: random label, generate, threshold, novelty, optional train.
Returns nil if any gate fails or sequence too short.
*/
func TryOnce(
	temperature float64,
	minRunes int,
	replayLen int,
	threshold float64,
	env Env,
) *Acceptance {
	if env.PickLabel == nil || env.GenerateContinuation == nil ||
		env.LabelConfidence == nil || env.PathIsNovel == nil || env.Train == nil {
		return nil
	}

	label := env.PickLabel()
	sequence := env.GenerateContinuation(label, temperature, replayLen)

	if utf8.RuneCountInString(sequence) < minRunes {
		return nil
	}

	confidence := env.LabelConfidence(sequence, label)

	if confidence <= threshold {
		return nil
	}

	if !env.PathIsNovel(sequence) {
		return nil
	}

	learningRate := env.LearningRate

	if learningRate <= 0 {
		learningRate = 1.0
	}

	env.Train(sequence, label, learningRate)

	return &Acceptance{
		Sequence:   sequence,
		Label:      label,
		Confidence: confidence,
	}
}
