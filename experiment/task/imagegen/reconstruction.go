package imagegen

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/provider/huggingface"
	"github.com/theapemachine/six/tokenizer"
)

/*
ReconstructionExperiment evaluates image reconstruction quality using the
cifar10 dataset, testing how well the system can model and reconstruct images.
*/
type ReconstructionExperiment struct {
	tableData []tools.ExperimentalData
	prose     []projector.ProseEntry
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

func NewReconstructionExperiment() *ReconstructionExperiment {
	experiment := &ReconstructionExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("uoft-cs/cifar10"),
			huggingface.DatasetWithSamples(2),
			huggingface.DatasetWithTextColumn("img"),
		),
	}

	experiment.prose = []projector.ProseEntry{
		{
			Condition: func() bool {
				return experiment.Score() > 0.5
			},
			Description: "It's alright.",
		},
	}

	return experiment
}

func (experiment *ReconstructionExperiment) Name() string {
	return "reconstruction"
}

func (experiment *ReconstructionExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *ReconstructionExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
	)

	return experiment.prompt
}

func (experiment *ReconstructionExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.RIGHT
}

func (experiment *ReconstructionExperiment) Section() string {
	return "imagegen"
}

func (experiment *ReconstructionExperiment) AddResult(results tools.ExperimentalData) {
	results.Scores = tools.ByteScores(results.Holdout, results.Observed)
	results.WeightedTotal = tools.WeightedTotal(results.Scores.Exact, results.Scores.Partial, results.Scores.Fuzzy)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *ReconstructionExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThanOrEqualTo, 0.0
}

func (experiment *ReconstructionExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}

	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}

	return total / float64(len(experiment.tableData))
}

func (experiment *ReconstructionExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *ReconstructionExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{}
}

func (experiment *ReconstructionExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
	return nil
}
