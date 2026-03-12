package phasedial

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/process"
	"github.com/theapemachine/six/provider"
)

/*
PhaseCoherenceExperiment performs pairwise phase correlation analysis across
all fingerprints in the corpus. It verifies the periodic and structural
properties of the PhaseDial encoding, such as short-range repulsion and
long-range attraction.
*/
type PhaseCoherenceExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *process.Prompt
}

func NewPhaseCoherenceExperiment() *PhaseCoherenceExperiment {
	return &PhaseCoherenceExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *PhaseCoherenceExperiment) Name() string {
	return "Phase Coherence"
}

func (experiment *PhaseCoherenceExperiment) Section() string {
	return "phasedial"
}

func (experiment *PhaseCoherenceExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *PhaseCoherenceExperiment) Prompts() *process.Prompt {
	experiment.prompt = process.NewPrompt(
		process.PromptWithDataset(experiment.dataset),
		process.PromptWithHoldout(experiment.Holdout()),
	)
	return experiment.prompt
}

func (experiment *PhaseCoherenceExperiment) Holdout() (int, process.HoldoutType) {
	return 0, process.RIGHT
}

func (experiment *PhaseCoherenceExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *PhaseCoherenceExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
}

func (experiment *PhaseCoherenceExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0.0 // No data yet
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *PhaseCoherenceExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *PhaseCoherenceExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{}
}
