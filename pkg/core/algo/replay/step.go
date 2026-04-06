package replay

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

	if len(sequence) < minRunes {
		return nil
	}

	confidence := env.LabelConfidence(sequence, label)

	if confidence <= threshold {
		return nil
	}

	if !env.PathIsNovel(sequence) {
		return nil
	}

	env.Train(sequence, label, 1)

	return &Acceptance{
		Sequence:   sequence,
		Label:      label,
		Confidence: confidence,
	}
}
