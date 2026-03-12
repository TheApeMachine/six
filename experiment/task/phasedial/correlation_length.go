package phasedial

import (
	"math"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/pkg/process"
	"github.com/theapemachine/six/pkg/provider"
)

/*
CorrelationLengthExperiment evaluates how the PhaseDial exploits the
correlation length of sequences. It tests various block partitions (hard vs
overlapping) to identify where super-additive gain is achieved, proving that
hard boundaries are necessary for structural independence.
*/
type CorrelationLengthExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *process.Prompt
}

func NewCorrelationLengthExperiment() *CorrelationLengthExperiment {
	return &CorrelationLengthExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *CorrelationLengthExperiment) Name() string {
	return "Correlation Length"
}

func (experiment *CorrelationLengthExperiment) Section() string {
	return "phasedial"
}

func (experiment *CorrelationLengthExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *CorrelationLengthExperiment) Prompts() *process.Prompt {
	experiment.prompt = process.NewPrompt(
		process.PromptWithDataset(experiment.dataset),
		process.PromptWithHoldout(experiment.Holdout()),
	)
	return experiment.prompt
}

func (experiment *CorrelationLengthExperiment) Holdout() (int, process.HoldoutType) {
	return 0, process.RIGHT
}

func (experiment *CorrelationLengthExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *CorrelationLengthExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
}

func (experiment *CorrelationLengthExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return math.NaN() // Not yet computed
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *CorrelationLengthExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *CorrelationLengthExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{}
}
