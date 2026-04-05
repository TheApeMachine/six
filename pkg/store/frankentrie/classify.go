package frankentrie

import "math"

/*
Classify scores a context against all labels and normalizes the result to
percentages with a softmax.
*/
func (store *Store) Classify(context string) map[string]float64 {
	scores := make(map[string]float64, len(store.labels))
	for _, label := range store.labels {
		scores[label] = 0
	}

	if len(store.labels) == 0 {
		return scores
	}

	tokens := store.Tokenize(context)
	expScores := make(map[string]float64, len(store.labels))
	maxLogProbability := math.Inf(-1)

	for _, label := range store.labels {
		classTotal := store.classTotals[label]

		if classTotal == 0 {
			classTotal = 0.1
		}

		logProbability := math.Log(classTotal / math.Max(float64(store.currentStep), 1))

		for tokenIndex := range tokens {
			contextStart := max(0, float64(tokenIndex-store.classificationContext))
			contextTokens := tokens[int(contextStart):tokenIndex]
			probabilities := store.interpolatedProbabilities(contextTokens, label)
			tokenProbability := probabilities[tokens[tokenIndex]]

			if tokenProbability <= 0 {
				tokenProbability = defaultUnknownProbability
			}

			logProbability += math.Log(tokenProbability)
		}

		scores[label] = logProbability

		if logProbability > maxLogProbability {
			maxLogProbability = logProbability
		}
	}

	sumExp := 0.0

	for _, label := range store.labels {
		expProbability := math.Exp(scores[label] - maxLogProbability)
		expScores[label] = expProbability
		sumExp += expProbability
	}

	for _, label := range store.labels {
		if sumExp == 0 {
			expScores[label] = 0
			continue
		}

		expScores[label] = expScores[label] / sumExp * 100
	}

	return expScores
}
