package experiment

import (
	"bytes"
	"math"

	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
	"github.com/theapemachine/six/vm"
)

type Scores struct {
	Exact   float64
	Partial float64
	Fuzzy   float64
}

type ExperimentalData struct {
	Idx           int
	Name          string
	Prefix        []byte
	Holdout       []byte
	Observed      []byte
	ErrorRatio    []byte
	Scores        Scores
	WeightedTotal float64
	TrueLabel     *int
	PredLabel     *int
}

type ScoreWeights struct {
	Exact   float64
	Partial float64
	Fuzzy   float64
}

type Result interface {
	Score() float64
}

func GetLoader(dataset provider.Dataset, lsmSpatialIndex float64) *vm.Loader {
	return vm.NewLoader(
		vm.LoaderWithStore(
			store.NewLSMSpatialIndex(lsmSpatialIndex),
		),
		vm.LoaderWithTokenizer(
			tokenizer.NewUniversal(
				tokenizer.TokenizerWithDataset(dataset),
			),
		),
	)
}

// countPrefixMatches returns the number of positions where expected[i] == retrieved[i]
// up to min(len(expected), len(retrieved)).
func countPrefixMatches(expected, retrieved []byte) int {
	matches := 0
	shorter := min(len(expected), len(retrieved))

	for i := range shorter {
		if expected[i] == retrieved[i] {
			matches++
		}
	}

	return matches
}

func ByteScores(expected, retrieved []byte) map[string]float64 {
	if len(expected) == 0 && len(retrieved) == 0 {
		return map[string]float64{
			"exact":   0,
			"partial": 0,
			"fuzzy":   0,
		}
	}

	var (
		exact   float64
		partial float64
		fuzzy   float64
	)

	// 1. Exact match - no excuses
	if bytes.Equal(expected, retrieved) {
		exact = 1.0
	}

	// 2. Partial match - correct bytes, no garbage penalty
	matches := countPrefixMatches(expected, retrieved)
	if len(expected) > 0 {
		partial = float64(matches) / float64(len(expected))
	}

	// 3. Fuzzy match - correct bytes, but extra garbage penalized
	longer := max(len(expected), len(retrieved))
	if longer > 0 {
		fuzzy = float64(matches) / float64(longer)
	}

	return map[string]float64{
		"exact":   exact,
		"partial": partial,
		"fuzzy":   fuzzy,
	}
}

func DefaultScoreWeights() ScoreWeights {
	return ScoreWeights{
		Exact:   1.0,
		Partial: 0.5,
		Fuzzy:   1.0 / 3.0,
	}
}

func WeightedByteScores(scores map[string]float64, weights ScoreWeights) float64 {
	return WeightedTotalWithWeights(
		weights,
		scores["exact"],
		scores["partial"],
		scores["fuzzy"],
	)
}

func normalizeWeights(weights ScoreWeights) ScoreWeights {
	if math.IsNaN(weights.Exact) || math.IsInf(weights.Exact, 0) {
		weights.Exact = 0
	}
	if math.IsNaN(weights.Partial) || math.IsInf(weights.Partial, 0) {
		weights.Partial = 0
	}
	if math.IsNaN(weights.Fuzzy) || math.IsInf(weights.Fuzzy, 0) {
		weights.Fuzzy = 0
	}

	if weights.Exact < 0 {
		weights.Exact = 0
	}
	if weights.Partial < 0 {
		weights.Partial = 0
	}
	if weights.Fuzzy < 0 {
		weights.Fuzzy = 0
	}

	total := weights.Exact + weights.Partial + weights.Fuzzy
	if total == 0 {
		return DefaultScoreWeights()
	}

	return weights
}

func WeightedTotalWithWeights(weights ScoreWeights, scores ...float64) float64 {
	if len(scores) == 0 {
		return 0.0
	}

	weights = normalizeWeights(weights)
	baseWeights := []float64{weights.Exact, weights.Partial, weights.Fuzzy}

	totalWeight := 0.0
	weightedSum := 0.0

	for i, score := range scores {
		weight := 0.0
		if i < len(baseWeights) {
			weight = baseWeights[i]
		} else {
			weight = 1.0 / float64(i+1)
		}

		if weight <= 0 {
			continue
		}

		weightedSum += score * weight
		totalWeight += weight
	}

	if totalWeight == 0 {
		return 0.0
	}

	return weightedSum / totalWeight
}

func WeightedTotal(scores ...float64) float64 {
	return WeightedTotalWithWeights(DefaultScoreWeights(), scores...)
}

func OptionalLabel(label int) *int {
	return &label
}
