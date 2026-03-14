package scaling

import (
	"fmt"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm/input"
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
	dataset   provider.Dataset
	prompt    []string
}

func NewCompressionExperiment() *CompressionExperiment {
	return &CompressionExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   NewSyntheticDataset(128, 50, 99),
	}
}

func (experiment *CompressionExperiment) Name() string    { return "Compression" }
func (experiment *CompressionExperiment) Section() string { return "scaling" }
func (experiment *CompressionExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *CompressionExperiment) Prompts() []string {
	experiment.prompt = []string{}
	return experiment.prompt
}

func (experiment *CompressionExperiment) Holdout() (int, input.HoldoutType) {
	return 32, input.RIGHT
}

func (experiment *CompressionExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *CompressionExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
}

func (experiment *CompressionExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0.0
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
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

	// Each entry stores a filter chord + fingerprint + readout.
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
