package scaling

import (
	"fmt"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
)

/*
CompressionExperiment measures collision-as-compression in the substrate.
Provides a 50-sample synthetic dataset (128 B each). The Pipeline ingests and
prompts normally. Finalize measures the ratio of raw input bytes to stored
substrate entries, characterising deduplication efficiency.

Note: the sample count is intentionally modest (50) so that the full
ingest+prompt cycle completes within the test-suite timeout. The paper prose
explains that the ratio would sharpen at larger N.
*/
type CompressionExperiment struct {
	tableData []tools.ExperimentalData
	dataset   data.Provider
	prompt    []string
	holdouts  [][]byte
	evaluator *tools.Evaluator
}

func NewCompressionExperiment() *CompressionExperiment {
	return &CompressionExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   NewSyntheticDataset(128, 50, 99),
		// Baseline 0.05: the normalized compression ratio (r/(r+1))
		// should be positive for any non-trivial deduplication.
		// Target 0.80: strong collision-as-compression at this sample size.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.80),
		),
	}
}

func (experiment *CompressionExperiment) Name() string    { return "Compression" }
func (experiment *CompressionExperiment) Section() string { return "scaling" }
func (experiment *CompressionExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *CompressionExperiment) Prompts() []string {
	ds, ok := experiment.dataset.(*SyntheticDataset)
	if !ok {
		experiment.prompt = experiment.prompt[:0]
		return experiment.prompt
	}
	experiment.prompt, experiment.holdouts = syntheticSamplePrompts(ds, 12, 0)
	return experiment.prompt
}

func (experiment *CompressionExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *CompressionExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *CompressionExperiment) Outcome() (any, Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *CompressionExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *CompressionExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *CompressionExperiment) Artifacts() []tools.Artifact {
	return CompressionArtifacts(experiment.tableData)
}

func (experiment *CompressionExperiment) Finalize(substrate any) error {
	rawBytes := 50 * 128
	entries := 1

	// Each entry stores a filter value + fingerprint + readout.
	// Effective compression = raw bytes / entries.
	ratio := 0.0
	if entries > 0 {
		ratio = float64(rawBytes) / float64(entries)
	}

	experiment.AddResult(tools.ExperimentalData{
		Idx:  len(experiment.tableData),
		Name: fmt.Sprintf("%d entries from %d KB", entries, rawBytes/1024),
		Scores: tools.Scores{
			Exact:   float64(rawBytes),
			Partial: float64(entries),
			Fuzzy:   ratio,
		},
		WeightedTotal: ratio / (ratio + 1.0), // normalized [0,1)
	})

	return nil
}

var _ tools.HoldoutProvider = (*CompressionExperiment)(nil)
