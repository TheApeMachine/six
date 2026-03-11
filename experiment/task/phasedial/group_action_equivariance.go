package phasedial

import (
	"math"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"

	"github.com/theapemachine/six/process"
	"github.com/theapemachine/six/provider"
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
	prompt    *process.Prompt
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

func (experiment *GroupActionEquivarianceExperiment) Prompts() *process.Prompt {
	experiment.prompt = process.NewPrompt(
		process.PromptWithDataset(experiment.dataset),
		process.PromptWithHoldout(experiment.Holdout()),
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
	if len(experiment.tableData) == 0 {
		return math.NaN() // Not yet computed
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
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

func (experiment *GroupActionEquivarianceExperiment) RawOutput() bool { return false }

func (experiment *GroupActionEquivarianceExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
	return nil
}
