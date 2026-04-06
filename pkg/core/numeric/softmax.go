package numeric

import "math"

/*
SoftmaxPercentages maps log-domain scores to percentages summing to 100 over labels
(max-subtraction for numerical stability).
*/
func SoftmaxPercentages(logEvidence map[string]float64, labels []string) map[string]float64 {
	expScores := make(map[string]float64, len(labels))
	maxLog := math.Inf(-1)

	for _, label := range labels {
		val, ok := logEvidence[label]

		if !ok {
			val = math.Inf(-1)
		}

		if val > maxLog {
			maxLog = val
		}
	}

	sumExp := 0.0

	for _, label := range labels {
		val, ok := logEvidence[label]

		if !ok {
			val = math.Inf(-1)
		}

		expProbability := math.Exp(val - maxLog)
		expScores[label] = expProbability
		sumExp += expProbability
	}

	out := make(map[string]float64, len(labels))

	if sumExp == 0 {
		for _, label := range labels {
			out[label] = 0
		}

		return out
	}

	for _, label := range labels {
		out[label] = expScores[label] / sumExp * 100
	}

	return out
}
