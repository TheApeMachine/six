package numeric

import (
	"fmt"
	"math"
	"strings"
)

/*
CosineSparseMaps is cosine similarity between sparse count maps (e.g. co-occurrence rows).
*/
func CosineSparseMaps(left map[string]float64, right map[string]float64) float64 {
	if left == nil || right == nil {
		return 0
	}

	dot := 0.0
	leftMag := 0.0
	rightMag := 0.0

	for token, count := range left {
		dot += count * right[token]
		leftMag += count * count
	}

	for _, count := range right {
		rightMag += count * count
	}

	if leftMag == 0 || rightMag == 0 {
		return 0
	}

	return dot / (math.Sqrt(leftMag) * math.Sqrt(rightMag))
}

/*
CharacterNgramCosine is cosine similarity over character n-gram counts (^/$ padded).
*/
func CharacterNgramCosine(left string, right string, n int) (float64, error) {
	if n <= 1 {
		return 0, fmt.Errorf("numeric: CharacterNgramCosine n must be >= 2, got %d", n)
	}

	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)

	if left == "" || right == "" {
		return 0, nil
	}

	leftRunes := []rune("^" + left + "$")
	rightRunes := []rune("^" + right + "$")

	if len(leftRunes) < n || len(rightRunes) < n {
		return 0, nil
	}

	leftCounts := make(map[string]float64)
	rightCounts := make(map[string]float64)

	for offset := 0; offset <= len(leftRunes)-n; offset++ {
		leftCounts[string(leftRunes[offset:offset+n])]++
	}

	for offset := 0; offset <= len(rightRunes)-n; offset++ {
		rightCounts[string(rightRunes[offset:offset+n])]++
	}

	dot := 0.0
	leftMag := 0.0
	rightMag := 0.0

	for gram, count := range leftCounts {
		dot += count * rightCounts[gram]
		leftMag += count * count
	}

	for _, count := range rightCounts {
		rightMag += count * count
	}

	if leftMag == 0 || rightMag == 0 {
		return 0, nil
	}

	return dot / (math.Sqrt(leftMag) * math.Sqrt(rightMag)), nil
}
