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
		if logEvidence[label] > maxLog {
			maxLog = logEvidence[label]
		}
	}

	sumExp := 0.0

	for _, label := range labels {
		expProbability := math.Exp(logEvidence[label] - maxLog)
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
