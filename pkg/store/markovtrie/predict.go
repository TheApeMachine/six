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

	store.mu.Lock()
	defer store.mu.Unlock()

	tokens := store.contentTokens(store.tokenizeUnlocked(data))
	if len(tokens) == 0 {
		return Prediction{}
	}

	surprisalSeries := store.surprisalSeriesBody(data)
	totalBits := 0.0
	for _, s := range surprisalSeries {
		totalBits += s.Bits
	}

	avgSurprisal := 0.0
	if len(surprisalSeries) > 0 {
		avgSurprisal = totalBits / float64(len(surprisalSeries))
	}

	if avgSurprisal > store.predictExperienceSurprisalGate() {
		store.experienceBody(data, nil)
	}

	scores := store.classifyBody(data)
	bestLabel, bestScore := store.bestLabelScore(scores)

	temperature := 0.7
	beamWidth := 3
	maxHops := 8

	if store.adaptive != nil {
		temperature = store.adaptive.deriveTemperature()
		beamWidth = store.adaptive.deriveBeamWidth(bestScore)
		maxHops = store.adaptive.deriveMaxHops(maxHops)
	}

	continuations := store.beamSearchBody(data, bestLabel, beamWidth, maxHops)

	sampled := store.generateBody(data, bestLabel, temperature, maxHops)
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
