package logic

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/provider/huggingface"
	"github.com/theapemachine/six/tokenizer"
)

/*
BabiExperiment evaluates question-answering performance using the
facebook/babi_qa dataset.
*/
type BabiExperiment struct {
	tableData []map[string]any
	prose     []projector.ProseEntry
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

func NewBabiExperiment() *BabiExperiment {
	experiment := &BabiExperiment{
		tableData: []map[string]any{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("facebook/babi_qa"),
			huggingface.DatasetWithSamples(100),
			huggingface.DatasetWithSubset("en-10k-qa1"),
			huggingface.DatasetWithTextColumn("story"),
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

func (experiment *BabiExperiment) Name() string {
	return "babi_benchmark"
}

func (experiment *BabiExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *BabiExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
	)

	return experiment.prompt
}

func (experiment *BabiExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.RIGHT
}

/*
AddResult should emperically prove that the system answered the question
correctly. It should compare the generated answer with the expected answer
and produce a score between 0 and 1.
*/
func (experiment *BabiExperiment) AddResult(results map[string]any) {
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
func (experiment *BabiExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.5
}

func (experiment *BabiExperiment) Score() float64 {
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

func (experiment *BabiExperiment) TableData() []map[string]any {
	return experiment.tableData
}
