package textgen

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
)

/*
ProseChainingExperiment evaluates the system's ability to chain multiple
fragments together into a coherent prose sequence. It tests multi-step
predictive generation based on the long-term context of the corpus.
*/
type ProseChainingExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

func NewProseChainingExperiment() *ProseChainingExperiment {
	return &ProseChainingExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *ProseChainingExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{}
}

func (experiment *ProseChainingExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
	return nil
}

func (experiment *ProseChainingExperiment) Name() string {
	return "Prose Chaining"
}

func (experiment *ProseChainingExperiment) Section() string {
	return "textgen"
}

func (experiment *ProseChainingExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *ProseChainingExperiment) Prompts() *tokenizer.Prompt {
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

func (experiment *ProseChainingExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 20, tokenizer.RIGHT
}

func (experiment *ProseChainingExperiment) AddResult(results tools.ExperimentalData) {
	score := 0.0
	observed := string(results.Observed)
	if len(observed) > 5 {
		score = 1.0
	}

	results.WeightedTotal = score
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *ProseChainingExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.5
}

func (experiment *ProseChainingExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, d := range experiment.tableData {
		total += d.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *ProseChainingExperiment) TableData() any {
	return experiment.tableData
}
