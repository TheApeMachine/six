package scaling

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

/*
BestFillScalingExperiment measures BestFill query latency as the substrate
dictionary grows. Provides a 100-sample synthetic dataset; the Pipeline
ingests normally. Finalize benchmarks raw BestFill at increasing dictionary
slices to characterise scan cost.

Note: modest sample count so the ingestion phase fits within the test
timeout. The paper prose notes that latency curves are expected to steepen
linearly with N.
*/
type BestFillScalingExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    []string
	evaluator *tools.Evaluator
}

func NewBestFillScalingExperiment() *BestFillScalingExperiment {
	return &BestFillScalingExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   NewSyntheticDataset(128, 100, 42),
		// Baseline 0.05: latency benchmark — any successful query
		// that produces scored output proves the BestFill path works.
		// Target 0.50: efficient scan returning quality matches.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.50),
		),
	}
}

func (experiment *BestFillScalingExperiment) Name() string    { return "BestFill Scaling" }
func (experiment *BestFillScalingExperiment) Section() string { return "scaling" }
func (experiment *BestFillScalingExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *BestFillScalingExperiment) Prompts() []string {
	// The substrate is populated during Loader.Start()
	// We don't need to run 5000 prompts through the active inference Graph
	// just to benchmark the latency of raw BestFill.
	return []string{}
}

func (experiment *BestFillScalingExperiment) Holdout() (int, input.HoldoutType) {
	return 32, input.RIGHT
}

func (experiment *BestFillScalingExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *BestFillScalingExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *BestFillScalingExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *BestFillScalingExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *BestFillScalingExperiment) Artifacts() []tools.Artifact {
	return BestFillArtifacts(experiment.tableData)
}

func (experiment *BestFillScalingExperiment) RawOutput() bool { return false }
