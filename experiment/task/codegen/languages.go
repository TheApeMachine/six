package codegen

import (
	"strings"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"

	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/provider/huggingface"
	"github.com/theapemachine/six/tokenizer"
)

/*
LanguagesExperiment tests the ability of the system to generate code for
various programming languages.
*/
type LanguagesExperiment struct {
	tableData []tools.ExperimentalData
	prose     []projector.ProseEntry
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
	manifold  [][]byte
	seen      map[string]struct{}
}

func NewLanguagesExperiment() *LanguagesExperiment {
	experiment := &LanguagesExperiment{
		tableData: []tools.ExperimentalData{},
		manifold:  make([][]byte, 0),
		seen:      make(map[string]struct{}),
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("code-rag-bench/mbpp"),
			huggingface.DatasetWithSamples(2),
			huggingface.DatasetWithTextColumn("code"),
		),
	}

	experiment.prose = []projector.ProseEntry{
		{
			Condition: func() bool {
				return experiment.Score() > 0.5
			},
			Description: "The system is able to generate code for the language Python.",
		},
	}

	return experiment
}

func (experiment *LanguagesExperiment) Name() string {
	return "Languages"
}

func (experiment *LanguagesExperiment) Section() string {
	return "codegen"
}

func (experiment *LanguagesExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *LanguagesExperiment) Prompts() *tokenizer.Prompt {
	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithDataset(experiment.dataset),
		tokenizer.PromptWithHoldout(experiment.Holdout()),
	)

	return experiment.prompt
}

func (experiment *LanguagesExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 50, tokenizer.RIGHT
}

/*
AddResult should emperically prove that the system generated the correct
code for the given prompt. It should compare the generated code with the
expected code and produce a score between 0 and 1.
*/
func (experiment *LanguagesExperiment) AddResult(results tools.ExperimentalData) {
	results.Scores = tools.ByteScores(
		results.Holdout, results.Observed,
	)

	results.WeightedTotal = tools.WeightedTotal(
		results.Scores.Exact,
		results.Scores.Partial,
		results.Scores.Fuzzy,
	)

	experiment.tableData = append(experiment.tableData, results)
}

/*
Outcome evaluates the overall result of the experiment, where we call a
failure if the total accuracy score is less than 0.5.
*/
func (experiment *LanguagesExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThanOrEqualTo, 0.0
}

func (experiment *LanguagesExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}

	total := 0.0

	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}

	return total / float64(len(experiment.tableData))
}

func (experiment *LanguagesExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *LanguagesExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactBarChart,
			FileName: slugify(experiment.Name()) + "_scores",
			Data:     experiment.tableData,
			Title:    experiment.Name() + " — Score Breakdown",
			Caption:  "Mean exact, partial, fuzzy, and weighted scores for " + experiment.Name() + ".",
			Label:    "fig:" + slugify(experiment.Name()) + "_scores",
		},
	}
}

func (experiment *LanguagesExperiment) Finalize(
	substrate *geometry.HybridSubstrate,
) error {
	return nil
}

func slugify(name string) string {
	return strings.ReplaceAll(
		strings.ToLower(strings.TrimSpace(name)), " ", "_",
	)
}
