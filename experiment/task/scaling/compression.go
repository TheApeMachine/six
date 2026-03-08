package scaling

import (
	"fmt"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
)

/*
CompressionExperiment measures collision-as-compression in the substrate.
Provides a 2000-sample synthetic dataset. The Pipeline ingests and prompts
normally. Finalize measures the ratio of raw input bytes to stored substrate
entries, characterizing deduplication efficiency.
*/
type CompressionExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

func NewCompressionExperiment() *CompressionExperiment {
	return &CompressionExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   NewSyntheticDataset(128, 2000, 99),
	}
}

func (experiment *CompressionExperiment) Name() string    { return "Compression" }
func (experiment *CompressionExperiment) Section() string { return "scaling" }
func (experiment *CompressionExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *CompressionExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
	)
	return experiment.prompt
}

func (experiment *CompressionExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 32, tokenizer.RIGHT
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
	return []tools.Artifact{
		{
			Type:     tools.ArtifactBarChart,
			FileName: "compression_scores",
			Data:     experiment.tableData,
			Title:    "Compression Ratios",
			Caption:  "Substrate deduplication efficiency for 2000 synthetic samples.",
			Label:    "fig:compression_ratios",
		},
	}
}

func (experiment *CompressionExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
	rawBytes := 2000 * 128
	entries := len(substrate.Entries)

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
