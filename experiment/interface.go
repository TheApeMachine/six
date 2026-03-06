package experiment

import (
	"bytes"

	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
	"github.com/theapemachine/six/vm"
)

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
	// Only scores the overlap, length difference doesn't matter
	shorter := min(len(expected), len(retrieved))
	if shorter > 0 {
		matches := 0

		for i := range shorter {
			if expected[i] == retrieved[i] {
				matches++
			}
		}

		partial = float64(matches) / float64(len(expected))
	}

	// 3. Fuzzy match - correct bytes, but extra garbage penalized
	// Penalizes retrieved being longer than expected
	if len(retrieved) > 0 {
		matches := 0
		shorter := min(len(expected), len(retrieved))

		for i := range shorter {
			if expected[i] == retrieved[i] {
				matches++
			}
		}

		fuzzy = float64(matches) / float64(max(len(expected), len(retrieved)))
	}

	return map[string]float64{
		"exact":   exact,
		"partial": partial,
		"fuzzy":   fuzzy,
	}
}

func WeightedTotal(scores ...float64) float64 {
	if len(scores) == 0 {
		return 0.0
	}

	totalWeight := 0.0
	weightedSum := 0.0

	for i, score := range scores {
		weight := 1.0 / float64(i+1)
		weightedSum += score * weight
		totalWeight += weight
	}

	return weightedSum / totalWeight
}
