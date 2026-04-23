package experiment

import (
	"bytes"
	"math"
	"strings"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/primitive"
)

type Scores struct {
	Exact   float64
	Partial float64
	Fuzzy   float64
}

type ExperimentalData struct {
	Idx               int
	Name              string
	Prompt            string
	Segments          []*primitive.Value
	Resolved          []*primitive.Value
	ClassLabels       []string
	Prefix            []byte
	Holdout           []byte
	Generation        []byte
	Classification    []byte
	ErrorRatio        []byte
	PropertiesWord    uint64
	ProbeState        uint64
	ProbeDepth        uint64
	ExecutionSettled  bool
	ReasoningResolved bool
	HaltedByCeiling   bool
	Scores            Scores
	WeightedTotal     float64
	TrueLabel         *int
	PredLabel         *int
}

func (data ExperimentalData) HasResolutionMetadata() bool {
	return data.ExecutionSettled ||
		data.ReasoningResolved ||
		data.HaltedByCeiling ||
		data.PropertiesWord != 0 ||
		data.ProbeState != 0 ||
		data.ProbeDepth != 0
}

func (data ExperimentalData) SemanticResolutionReady() bool {
	if !data.HasResolutionMetadata() {
		return true
	}

	return data.ReasoningResolved
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

/*
PipelineExperiment is the contract for paper/eval pipelines.

Validation is intentionally two-tier:

 1. Per-prompt (optional): HoldoutForPrompt(idx) may return (nil, false). That
    means there is no byte-level target for that prompt — the harness still runs
    the prompt and appends a scored row via AddResult so aggregate Score() is
    meaningful. When it returns (bytes, true), callers may assert exact readout
    against those bytes in addition to recording AddResult.

 2. Aggregate: Score() / Outcome() are driven by table rows accumulated in
    AddResult (and optionally Finalize on experiments that implement it). Tests
    must record one ExperimentalData row per prompt (Generation = substrate
    readout) or Outcome() stays at zero from an empty table.

 3. Per-prompt: OutcomeForPrompt(idx) mirrors Outcome()’s threshold but scores
    only the row at idx (OutcomeForPromptConvey / OutcomeForTableRow), so
    pipeline tests can show expected-vs-reality per sample instead of the
    cumulative aggregate alone.
*/
type PipelineExperiment interface {
	Name() string
	Section() string
	Dataset() data.Provider
	Prompts() []string
	HoldoutForPrompt(idx int) ([]byte, bool)
	LabelForPrompt(idx int) []byte
	AddResult(ExperimentalData)
	Outcome() (any, Assertion, any)
	OutcomeForPrompt(idx int) (any, Assertion, any)
	TableData() any
	Artifacts() []Artifact
}

/*
SummaryHoldoutDescriptor lets an experiment describe how supervision holdout
is defined for the paper summary row. When absent, the projector uses a short
generic caption.
*/
type SummaryHoldoutDescriptor interface {
	SummaryHoldoutDescription() string
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
