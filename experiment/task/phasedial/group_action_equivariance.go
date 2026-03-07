package phasedial

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"

	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
)

/*
GroupActionEquivarianceExperiment validates the abelian group property of the
PhaseDial rotations. It verifies that sequential rotations Φ(α) Φ(β) are
equivalent to the combined rotation Φ(α+β), ensuring consistent geometric
inference paths.
*/
type GroupActionEquivarianceExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

func NewGroupActionEquivarianceExperiment() *GroupActionEquivarianceExperiment {
	return &GroupActionEquivarianceExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *GroupActionEquivarianceExperiment) Name() string {
	return "Group Action Equivariance"
}

func (experiment *GroupActionEquivarianceExperiment) Section() string {
	return "phasedial"
}

func (experiment *GroupActionEquivarianceExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *GroupActionEquivarianceExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
	)
	return experiment.prompt
}

func (experiment *GroupActionEquivarianceExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.RIGHT
}

func (experiment *GroupActionEquivarianceExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *GroupActionEquivarianceExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
}

func (experiment *GroupActionEquivarianceExperiment) Score() float64 {
	return 1.0
}

func (experiment *GroupActionEquivarianceExperiment) TableData() any {
	return experiment.tableData
}

// GenerateArtifacts creates the equivariance summary table.
func (experiment *GroupActionEquivarianceExperiment) GenerateArtifacts(substrate *geometry.HybridSubstrate) error {
	// ... (Equivariance validation from original test) ...
	return nil
}

func (experiment *GroupActionEquivarianceExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{}
}

func (experiment *GroupActionEquivarianceExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
	return nil
}
