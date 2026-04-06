package infer

import "github.com/theapemachine/six/pkg/core/algo/beam"

/*
Continuation re-exports beam.BeamContinuation to avoid duplicate types.
*/
type Continuation = beam.BeamContinuation

/*
Outcome is the unified inference product: winner label, scores, and ranked paths.
*/
type Outcome struct {
	Label         string
	Confidence    float64
	Scores        map[string]float64
	Continuations []Continuation
}

/*
Env wires trie-local or model-local behavior without this package importing a store.
*/
type Env struct {
	ShouldSkip        func(data string) bool
	MeanSurprisalBits func(data string) float64
	SurprisalGate     float64
	LearnFromSurprise func(data string)
	Classify          func(data string) map[string]float64
	BestLabel         func(
		scores map[string]float64,
	) (label string, confidence float64)
	InferenceParams func(
		confidence float64,
	) (temperature float64, beamWidth int, maxHops int)
	BeamSearch func(
		data string, label string, beamWidth int, maxHops int,
	) []Continuation
	Generate func(
		data string, label string, temperature float64, maxHops int,
	) string
}

/*
Run executes the predict-time pipeline: optional surprise learning, classify,
derive widths, beam + sample, merge distinct sample into beams.
*/
func Run(data string, env Env) Outcome {
	if env.ShouldSkip != nil && env.ShouldSkip(data) {
		return Outcome{}
	}

	meanBits := 0.0

	if env.MeanSurprisalBits != nil {
		meanBits = env.MeanSurprisalBits(data)
	}

	if meanBits > env.SurprisalGate && env.LearnFromSurprise != nil {
		env.LearnFromSurprise(data)
	}

	scores := env.Classify(data)
	bestLabel, bestScore := env.BestLabel(scores)

	temperature := 0.7
	beamWidth := 3
	maxHops := 8

	if env.InferenceParams != nil {
		temperature, beamWidth, maxHops = env.InferenceParams(bestScore)
	}

	continuations := env.BeamSearch(data, bestLabel, beamWidth, maxHops)
	sampled := env.Generate(data, bestLabel, temperature, maxHops)
	continuations = appendIfNewSequence(continuations, sampled, 0)

	return Outcome{
		Label:         bestLabel,
		Confidence:    bestScore,
		Scores:        scores,
		Continuations: continuations,
	}
}

func appendIfNewSequence(
	continuations []Continuation,
	sampled string,
	sampleScore float64,
) []Continuation {
	if sampled == "" {
		return continuations
	}

	for _, candidate := range continuations {
		if candidate.Sequence == sampled {
			return continuations
		}
	}

	return append(continuations, Continuation{
		Sequence: sampled,
		Score:    sampleScore,
	})
}
