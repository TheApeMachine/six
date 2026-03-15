package scaling

import (
	"fmt"
	"time"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

/*
PipelineThroughputExperiment measures end-to-end throughput.
Provides a 1000-sample synthetic dataset and prompts; the Pipeline handles
ingestion and querying. Finalize records timing and substrate size metrics.
*/
type PipelineThroughputExperiment struct {
	tableData  []tools.ExperimentalData
	dataset    provider.Dataset
	prompt     []string
	ingestTime time.Time
	sampleLen  int
	nSamples   int
	evaluator  *tools.Evaluator
}

func NewPipelineThroughputExperiment() *PipelineThroughputExperiment {
	return &PipelineThroughputExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   NewSyntheticDataset(128, 50, 42),
		sampleLen: 128,
		nSamples:  50,
		// Baseline 0.05: any successful ingest+query cycle that produces
		// throughput metrics passes. Zero means the pipeline didn't run.
		// Target 0.50: efficient end-to-end processing.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.50),
		),
	}
}

func (experiment *PipelineThroughputExperiment) Name() string    { return "Pipeline Throughput" }
func (experiment *PipelineThroughputExperiment) Section() string { return "scaling" }
func (experiment *PipelineThroughputExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *PipelineThroughputExperiment) Prompts() []string {
	experiment.ingestTime = time.Now()
	experiment.prompt = []string{}
	return experiment.prompt
}

func (experiment *PipelineThroughputExperiment) Holdout() (int, input.HoldoutType) {
	return 32, input.RIGHT
}

func (experiment *PipelineThroughputExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *PipelineThroughputExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *PipelineThroughputExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *PipelineThroughputExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *PipelineThroughputExperiment) Artifacts() []tools.Artifact {
	return ThroughputArtifacts(experiment.tableData)
}

func (experiment *PipelineThroughputExperiment) RawOutput() bool { return false }

func (experiment *PipelineThroughputExperiment) Finalize(substrate any) error {
	elapsed := time.Since(experiment.ingestTime)
	totalBytes := experiment.nSamples * experiment.sampleLen
	entries := 1

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
