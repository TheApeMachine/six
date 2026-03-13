package experiment

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWeightedTotalWithWeightsBiasesConfiguredSignals(t *testing.T) {
	scores := map[string]float64{
		"exact":   0.0,
		"partial": 1.0,
		"fuzzy":   0.0,
	}

	baseline := WeightedTotal(scores["exact"], scores["partial"], scores["fuzzy"])
	calibrated := WeightedByteScores(scores, ScoreWeights{Exact: 0.1, Partial: 0.8, Fuzzy: 0.1})

	require.Greater(t, calibrated, baseline)
	require.InDelta(t, 0.8, calibrated, 1e-12)
}

func TestWeightedTotalWithWeightsFallsBackToDefaults(t *testing.T) {
	scores := []float64{0.9, 0.3, 0.6}

	baseline := WeightedTotal(scores...)
	zeroWeights := WeightedTotalWithWeights(ScoreWeights{}, scores...)
	negativeWeights := WeightedTotalWithWeights(ScoreWeights{Exact: -1.0, Partial: -2.0, Fuzzy: -3.0}, scores...)

	require.InDelta(t, baseline, zeroWeights, 1e-12)
	require.InDelta(t, baseline, negativeWeights, 1e-12)
}

func TestWeightedTotalWithWeightsSanitizesNaN(t *testing.T) {
	scores := []float64{0.9, 0.3, 0.6}

	result := WeightedTotalWithWeights(ScoreWeights{Exact: math.NaN(), Partial: 1.0, Fuzzy: 0.0}, scores...)

	require.False(t, math.IsNaN(result))
	require.InDelta(t, 0.3, result, 1e-12)
}

func TestByteScoresPenalizeGarbageAroundContainedSpan(t *testing.T) {
	scores := ByteScores([]byte("bathroom"), []byte("Mary is in the bathroom now"))

	require.Equal(t, 0.0, scores.Exact)
	require.Equal(t, 1.0, scores.Partial)
	require.Less(t, scores.Fuzzy, 1.0)
	require.True(t, ByteSpanMatch([]byte("bathroom"), []byte("Mary is in the bathroom now")))
}

func TestByteSpanMatchRejectsIncidentalPrefixResidue(t *testing.T) {
	require.False(t, ByteSpanMatch([]byte("bathroom"), []byte("ba")))
}
