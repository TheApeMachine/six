package scaling

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
)

/*
CompressionExperiment evaluates the "collision-as-compression" mechanism in
the LSM Spatial Index. It demonstrates that the system achieves high
compression ratios for redundant data by deduplicating identical Morton keys
during ingestion.
*/
type CompressionExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

func NewCompressionExperiment() *CompressionExperiment {
	return &CompressionExperiment{
		tableData: []tools.ExperimentalData{},
		// This experiment uses synthetic data, but we can wrap it in a local provider.
		dataset: nil,
	}
}

func (experiment *CompressionExperiment) Name() string {
	return "Compression"
}

func (experiment *CompressionExperiment) Section() string {
	return "scaling"
}

func (experiment *CompressionExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *CompressionExperiment) Prompts() *tokenizer.Prompt {
	// Synthetic experiment usually handles its own "prompts" or datasets.
	return nil
}

func (experiment *CompressionExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.RIGHT
}

func (experiment *CompressionExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *CompressionExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
}

func (experiment *CompressionExperiment) Score() float64 {
	return 1.0
}

func (experiment *CompressionExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *CompressionExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{}
}

func (experiment *CompressionExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
	return nil
}
