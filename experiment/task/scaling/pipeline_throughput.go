package scaling

import (
	"fmt"
	"time"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/process"
	"github.com/theapemachine/six/provider"
)

/*
PipelineThroughputExperiment measures end-to-end throughput.
Provides a 1000-sample synthetic dataset and prompts; the Pipeline handles
ingestion and querying. Finalize records timing and substrate size metrics.
*/
type PipelineThroughputExperiment struct {
	tableData  []tools.ExperimentalData
	dataset    provider.Dataset
	prompt     *process.Prompt
	ingestTime time.Time
	sampleLen  int
	nSamples   int
}

func NewPipelineThroughputExperiment() *PipelineThroughputExperiment {
	return &PipelineThroughputExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   NewSyntheticDataset(128, 50, 42),
		sampleLen: 128,
		nSamples:  50,
	}
}

func (experiment *PipelineThroughputExperiment) Name() string    { return "Pipeline Throughput" }
func (experiment *PipelineThroughputExperiment) Section() string { return "scaling" }
func (experiment *PipelineThroughputExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *PipelineThroughputExperiment) Prompts() *process.Prompt {
	experiment.ingestTime = time.Now()
	experiment.prompt = process.NewPrompt(
		process.PromptWithDataset(experiment.dataset),
		process.PromptWithHoldout(experiment.Holdout()),
	)
	return experiment.prompt
}

func (experiment *PipelineThroughputExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 32, tokenizer.RIGHT
}

func (experiment *PipelineThroughputExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *PipelineThroughputExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
}

func (experiment *PipelineThroughputExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0.0
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *PipelineThroughputExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *PipelineThroughputExperiment) Artifacts() []tools.Artifact {
	return ThroughputArtifacts(experiment.tableData)
}

func (experiment *PipelineThroughputExperiment) RawOutput() bool { return false }

func (experiment *PipelineThroughputExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
	elapsed := time.Since(experiment.ingestTime)
	totalBytes := experiment.nSamples * experiment.sampleLen
	entries := len(substrate.Entries)

	kbPerSec := 0.0
	if elapsed.Milliseconds() > 0 {
		kbPerSec = (float64(totalBytes) / 1024.0) / (float64(elapsed.Milliseconds()) / 1000.0)
	}

	experiment.AddResult(tools.ExperimentalData{
		Idx:  len(experiment.tableData),
		Name: fmt.Sprintf("Summary: %d entries, %.0f KB/s", entries, kbPerSec),
		Scores: tools.Scores{
			Exact:   kbPerSec,
			Partial: float64(entries),
			Fuzzy:   float64(elapsed.Milliseconds()),
		},
		WeightedTotal: kbPerSec / (kbPerSec + 100.0),
	})

	return nil
}
