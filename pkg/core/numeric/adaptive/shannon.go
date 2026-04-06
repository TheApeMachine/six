package adaptive

import "math"

/*
ShannonEntropy calculates the Shannon entropy of a vector of 64-bit values.
*/
func ShannonEntropy(vector [8]uint64) float64 {
	var entropy float64

	for _, value := range vector {
		probability := float64(value) / 64.0

		if probability > 0 {
			entropy -= probability * math.Log2(probability)
		}
	}

	return entropy
}

/*
ShannonEntropyBitsFromMap sums -p log2(p) for map values interpreted as
nonnegative quantities scaled into probabilities by probabilityScale.
Classifier scores in this repo are percentages; callers pass 1/100 for scale.
*/
func ShannonEntropyBitsFromMap(scores map[string]float64, probabilityScale float64) float64 {
	var entropy float64

	for _, raw := range scores {
		probability := raw * probabilityScale

		if probability > 0 {
			entropy -= probability * math.Log2(probability)
		}
	}

	return entropy
}
