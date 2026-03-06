package imagegen

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/provider/huggingface"
	"github.com/theapemachine/six/tokenizer"
)

/*
ReconstructionExperiment evaluates image reconstruction quality using the
cifar10 dataset, testing how well the system can model and reconstruct images.
*/
type ReconstructionExperiment struct {
	tableData []map[string]any
	prose     []projector.ProseEntry
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

func NewReconstructionExperiment() *ReconstructionExperiment {
	experiment := &ReconstructionExperiment{
		tableData: []map[string]any{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("uoft-cs/cifar10"),
			huggingface.DatasetWithSamples(100),
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

/*
AddResult should emperically prove that the system reconstructed the correct
image for the given prompt. It should compare the generated output with the
expected output and produce a score between 0 and 1.
*/
func (experiment *ReconstructionExperiment) AddResult(results map[string]any) {
	var prompt, res []byte

	if val, ok := results["prompt"]; ok {
		if b, ok := val.([]byte); ok {
			prompt = b
		}
	}

	if val, ok := results["result"]; ok {
		if b, ok := val.([]byte); ok {
			res = b
		}
	}

	results["scores"] = tools.ByteScores(prompt, res)
	experiment.tableData = append(experiment.tableData, results)
}

/*
Outcome evaluates the overall result of the experiment, where we call a
failure if the total accuracy score is less than 0.5.
*/
func (experiment *ReconstructionExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.5
}

func (experiment *ReconstructionExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}

	total := 0.0

	for _, data := range experiment.tableData {
		scoresVal, ok := data["scores"]
		if !ok {
			continue
		}

		scores, ok := scoresVal.(map[string]float64)
		if !ok {
			continue
		}

		total += tools.WeightedTotal(
			scores["exact"],
			scores["partial"],
			scores["fuzzy"],
		)
	}

	return total / float64(len(experiment.tableData))
}

func (experiment *ReconstructionExperiment) TableData() []map[string]any {
	return experiment.tableData
}
