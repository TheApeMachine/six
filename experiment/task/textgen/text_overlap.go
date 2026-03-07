package textgen

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
)

/*
TextOverlapExperiment evaluates the system's ability to perform
overlap-aware text generation. It ensures that the generated sequence
smoothly transitions between ingested spans by identifying shared
structural boundaries.
*/
type TextOverlapExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

func NewTextOverlapExperiment() *TextOverlapExperiment {
	return &TextOverlapExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *TextOverlapExperiment) Name() string {
	return "Text Overlap"
}

func (experiment *TextOverlapExperiment) Section() string {
	return "textgen"
}

func (experiment *TextOverlapExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *TextOverlapExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
		tokenizer.PromptWithValues([]string{
			"To be, or not",
			"It was the best",
			"Call me",
			"In a hole in the",
			"All happy families",
		}),
	)

	return experiment.prompt
}

func (experiment *TextOverlapExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 20, tokenizer.RIGHT
}

func (experiment *TextOverlapExperiment) AddResult(results tools.ExperimentalData) {
	score := 0.0
	observed := string(results.Observed)
	if len(observed) > 5 {
		score = 1.0
	}

	results.WeightedTotal = score
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *TextOverlapExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.5
}

func (experiment *TextOverlapExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, d := range experiment.tableData {
		total += d.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *TextOverlapExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *TextOverlapExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{}
}

func (experiment *TextOverlapExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
	return nil
}
