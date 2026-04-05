package markovtrie

/*
Predict is the unified entry point for inference. The caller passes data and
gets back classification and continuations. All internal parameters (temperature,
beam width, generation length) are derived from the trie's adaptive state.
Surprising inputs are learned automatically.
*/
func (store *Store) Predict(data string) Prediction {
	if store == nil {
		return Prediction{}
	}

	tokens := store.contentTokens(store.Tokenize(data))
	if len(tokens) == 0 {
		return Prediction{}
	}

	// Surprisal drives internal learning decisions.
	surprisalSeries := store.SurprisalSeries(data)
	totalBits := 0.0
	for _, s := range surprisalSeries {
		totalBits += s.Bits
	}

	avgSurprisal := 0.0
	if len(surprisalSeries) > 0 {
		avgSurprisal = totalBits / float64(len(surprisalSeries))
	}

	// Learn from surprising input before classifying.
	if avgSurprisal > 1.0 {
		store.Experience(data, nil)
	}

	// Classify.
	scores := store.Classify(data)
	bestLabel, bestScore := store.bestLabelScore(scores)

	// Derive generation parameters.
	temperature := 0.7
	beamWidth := 3
	maxHops := 8

	if store.adaptive != nil {
		temperature = store.adaptive.deriveTemperature()
		beamWidth = store.adaptive.deriveBeamWidth(bestScore)
		maxHops = store.adaptive.deriveMaxHops(maxHops)
	}

	// Continuations via beam search + one sampled generation for diversity.
	continuations := store.BeamSearch(data, bestLabel, beamWidth, maxHops)

	sampled := store.Generate(data, bestLabel, temperature, maxHops)
	if sampled != "" {
		isDuplicate := false
		for _, c := range continuations {
			if c.Sequence == sampled {
				isDuplicate = true
				break
			}
		}

		if !isDuplicate {
			continuations = append(continuations, BeamCandidate{
				Sequence: sampled,
				Score:    0,
			})
		}
	}

	return Prediction{
		Label:         bestLabel,
		Confidence:    bestScore,
		Scores:        scores,
		Continuations: continuations,
	}
}
