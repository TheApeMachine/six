package experiment

import (
	"bytes"
	"math"
	"strings"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/process"
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

type ArtifactType string

const (
	ArtifactTable           ArtifactType = "table"
	ArtifactBarChart        ArtifactType = "bar"
	ArtifactLineChart       ArtifactType = "line"
	ArtifactComboChart      ArtifactType = "combo"
	ArtifactHeatMap         ArtifactType = "heatmap"
	ArtifactConfusionMatrix ArtifactType = "confusion"
	ArtifactMultiPanel      ArtifactType = "multipanel"
	ArtifactProse           ArtifactType = "prose"
	ArtifactImageStrip      ArtifactType = "imagestrip"
	ArtifactPolarConstraint ArtifactType = "polarconstraint"
)

type Artifact struct {
	Type     ArtifactType
	FileName string
	Data     any
	Title    string
	Caption  string
	Label    string
}

type Result interface {
	Score() float64
}

type PipelineExperiment interface {
	Name() string
	Section() string
	Dataset() provider.Dataset
	Prompts() *process.Prompt
	Holdout() (int, process.HoldoutType)
	AddResult(ExperimentalData)
	Outcome() (any, gc.Assertion, any)
	TableData() any
	Artifacts() []Artifact
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

func ByteScores(expected, retrieved []byte) Scores {
	if len(expected) == 0 && len(retrieved) == 0 {
		return Scores{
			Exact:   0,
			Partial: 0,
			Fuzzy:   0,
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

	if contains, coverage := containedSpanCoverage(expected, retrieved); contains {
		partial = max(partial, 1.0)
		fuzzy = max(fuzzy, coverage)
	}

	return Scores{
		Exact:   exact,
		Partial: partial,
		Fuzzy:   fuzzy,
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

func ByteSpanMatch(expected, retrieved []byte) bool {
	if bytes.Equal(expected, retrieved) {
		return true
	}

	contains, _ := containedSpanCoverage(expected, retrieved)

	return contains
}

func containedSpanCoverage(expected, retrieved []byte) (bool, float64) {
	if len(expected) == 0 || len(retrieved) == 0 {
		return false, 0
	}

	expectedText := strings.TrimSpace(strings.ToLower(string(expected)))
	retrievedText := strings.TrimSpace(strings.ToLower(string(retrieved)))

	if expectedText == "" || retrievedText == "" || !strings.Contains(retrievedText, expectedText) {
		return false, 0
	}

	return true, float64(len(expectedText)) / float64(max(len(retrievedText), len(expectedText)))
}

func OptionalLabel(label int) *int {
	return &label
}

func Slugify(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "_")
}
