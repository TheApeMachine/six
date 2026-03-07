package logic

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
BabiExperiment evaluates question-answering performance using the
facebook/babi_qa dataset.
*/
type BabiExperiment struct {
	tableData []tools.ExperimentalData
	prose     []projector.ProseEntry
	dataset   *huggingface.BabiQADataset
	prompt    *tokenizer.Prompt
}

func NewBabiExperiment() *BabiExperiment {
	experiment := &BabiExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.NewBabiQA(
			huggingface.DatasetWithRepo("facebook/babi_qa"),
			huggingface.DatasetWithSamples(100),
			huggingface.DatasetWithSubset("en-10k-qa1"),
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
	samples, err := experiment.dataset.Samples()
	if err != nil {
		return tokenizer.NewPrompt()
	}

	promptSamples := make([]tokenizer.PromptSample, 0, len(samples))
	for _, sample := range samples {
		promptSamples = append(promptSamples, tokenizer.PromptSample{
			Visible: sample.Visible,
			HeldOut: sample.Answer,
			Full:    sample.Full,
		})
	}

	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithSamples(promptSamples),
	)

	return experiment.prompt
}

func (experiment *BabiExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.RIGHT
}

func (experiment *BabiExperiment) Section() string {
	return "logic"
}

func (experiment *BabiExperiment) AddResult(results tools.ExperimentalData) {
	results.Scores = tools.ByteScores(results.Holdout, results.Observed)
	results.WeightedTotal = tools.WeightedTotal(results.Scores.Exact, results.Scores.Partial, results.Scores.Fuzzy)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *BabiExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
}

func (experiment *BabiExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}

	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}

	return total / float64(len(experiment.tableData))
}

func (experiment *BabiExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *BabiExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{}
}

func (experiment *BabiExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
	return nil
}
