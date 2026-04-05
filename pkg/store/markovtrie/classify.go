package markovtrie

import "math"

/*
Classify scores a context against all labels and normalizes the result to
percentages with a softmax.
*/
func (store *Store) Classify(context string) map[string]float64 {
	scores, _ := store.classifyLogEvidence(context)
	result := softmaxPercentages(scores, store.labels)

	if store.adaptive != nil {
		store.adaptive.observeClassificationEntropy(result)
	}

	return result
}

/*
ClassifyDetailed returns softmax percentages plus per-label token log-prob traces
(including a synthetic PRIOR row) for contrastive explanations like the demo UI.
*/
func (store *Store) ClassifyDetailed(context string) (
	scores map[string]float64,
	contributions map[string][]TokenContribution,
) {
	logEvidence, contributions := store.classifyLogEvidence(context)

	return softmaxPercentages(logEvidence, store.labels), contributions
}

func (store *Store) classifyLogEvidence(context string) (
	logEvidence map[string]float64,
	contributions map[string][]TokenContribution,
) {
	logEvidence = make(map[string]float64, len(store.labels))
	contributions = make(map[string][]TokenContribution, len(store.labels))

	for _, label := range store.labels {
		logEvidence[label] = 0
		contributions[label] = nil
	}

	if len(store.labels) == 0 {
		return logEvidence, contributions
	}

	tokens := store.Tokenize(context)

	classificationContext := store.classificationContext
	if store.adaptive != nil {
		classificationContext = store.adaptive.adaptiveClassificationContext(classificationContext)
	}

	for _, label := range store.labels {
		classTotal := store.ClassTotals[label]

		if classTotal == 0 {
			classTotal = 0.1
		}

		logProbability := math.Log(classTotal / math.Max(float64(store.currentStep), 1))
		trace := []TokenContribution{
			{Token: "PRIOR", LogProb: logProbability},
		}

		for tokenIndex := range tokens {
			contextStart := tokenIndex - classificationContext
			if contextStart < 0 {
				contextStart = 0
			}

			contextTokens := tokens[contextStart:tokenIndex]
			probabilities := store.interpolatedProbabilities(contextTokens, label)
			tokenProbability := probabilities[tokens[tokenIndex]]

			if tokenProbability <= 0 {
				tokenProbability = defaultUnknownProbability
			}

			lp := math.Log(tokenProbability)
			logProbability += lp
			trace = append(trace, TokenContribution{
				Token:   tokens[tokenIndex],
				LogProb: lp,
			})
		}

		logEvidence[label] = logProbability
		contributions[label] = trace
	}

	return logEvidence, contributions
}

func softmaxPercentages(
	logEvidence map[string]float64,
	labels []string,
) map[string]float64 {
	expScores := make(map[string]float64, len(labels))
	maxLogProbability := math.Inf(-1)

	for _, label := range labels {
		if logEvidence[label] > maxLogProbability {
			maxLogProbability = logEvidence[label]
		}
	}

	sumExp := 0.0

	for _, label := range labels {
		expProbability := math.Exp(logEvidence[label] - maxLogProbability)
		expScores[label] = expProbability
		sumExp += expProbability
	}

	out := make(map[string]float64, len(labels))

	for _, label := range labels {
		if sumExp == 0 {
			out[label] = 0
			continue
		}

		out[label] = expScores[label] / sumExp * 100
	}

	return out
}
