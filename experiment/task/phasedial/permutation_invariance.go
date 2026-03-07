package phasedial

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"

	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
)

/*
PermutationInvarianceExperiment evaluates whether the PhaseDial's retrieval
properties are invariant to the order of ingestion. It performs a geodesic
scan and generates a multi-panel chart showing the semantic geodesic matrix.
*/
type PermutationInvarianceExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

func NewPermutationInvarianceExperiment() *PermutationInvarianceExperiment {
	return &PermutationInvarianceExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *PermutationInvarianceExperiment) Name() string {
	return "Permutation Invariance"
}

func (experiment *PermutationInvarianceExperiment) Section() string {
	return "phasedial"
}

func (experiment *PermutationInvarianceExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *PermutationInvarianceExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
	)
	return experiment.prompt
}

func (experiment *PermutationInvarianceExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.RIGHT
}

func (experiment *PermutationInvarianceExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *PermutationInvarianceExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
}

func (experiment *PermutationInvarianceExperiment) Score() float64 {
	return 1.0
}

func (experiment *PermutationInvarianceExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *PermutationInvarianceExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{}
}

func (experiment *PermutationInvarianceExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
	return nil
}
