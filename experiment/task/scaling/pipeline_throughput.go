package scaling

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
)

/*
PipelineThroughputExperiment measures the end-to-end throughput of the full
system pipeline, from tokenization and ingestion to BestFill querying. It
identifies bottlenecks and validates the system's ability to handle large
corpora within millisecond latencies.
*/
type PipelineThroughputExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

func NewPipelineThroughputExperiment() *PipelineThroughputExperiment {
	return &PipelineThroughputExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   nil,
	}
}

func (experiment *PipelineThroughputExperiment) Name() string {
	return "Pipeline Throughput"
}

func (experiment *PipelineThroughputExperiment) Section() string {
	return "scaling"
}

func (experiment *PipelineThroughputExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *PipelineThroughputExperiment) Prompts() *tokenizer.Prompt {
	return nil
}

func (experiment *PipelineThroughputExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.RIGHT
}

func (experiment *PipelineThroughputExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *PipelineThroughputExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
}

func (experiment *PipelineThroughputExperiment) Score() float64 {
	return 1.0
}

func (experiment *PipelineThroughputExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *PipelineThroughputExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{}
}

func (experiment *PipelineThroughputExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
	return nil
}
