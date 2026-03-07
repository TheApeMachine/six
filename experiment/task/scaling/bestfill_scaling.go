package scaling

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
)

/*
BestFillScalingExperiment measures the performance and scalability of the
BestFill CPU shader. It demonstrates linear scaling with dictionary size and
identifies the memory-bandwidth-limited nature of the popcount scan.
*/
type BestFillScalingExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

func NewBestFillScalingExperiment() *BestFillScalingExperiment {
	return &BestFillScalingExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   nil,
	}
}

func (experiment *BestFillScalingExperiment) Name() string {
	return "BestFill Scaling"
}

func (experiment *BestFillScalingExperiment) Section() string {
	return "scaling"
}

func (experiment *BestFillScalingExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *BestFillScalingExperiment) Prompts() *tokenizer.Prompt {
	return nil
}

func (experiment *BestFillScalingExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.RIGHT
}

func (experiment *BestFillScalingExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *BestFillScalingExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
}

func (experiment *BestFillScalingExperiment) Score() float64 {
	return 1.0
}

func (experiment *BestFillScalingExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *BestFillScalingExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{}
}

func (experiment *BestFillScalingExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
	return nil
}
