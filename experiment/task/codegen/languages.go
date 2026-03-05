package codegen

import (
	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/experiment"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/provider/huggingface"
	"github.com/theapemachine/six/vm"
)

/*
LanguagesExperiment tests the ability of the system to generate code for
various programming languages.
*/
type LanguagesExperiment struct {
	results   experiment.Result
	tableData []map[string]any
	prose     []projector.ProseEntry
	Results   []vm.SpanResult
	dataset   provider.Dataset
}

type mockResult struct{}

func (m *mockResult) Score() float64 { return 1.0 }

func NewLanguagesExperiment() *LanguagesExperiment {
	experiment := &LanguagesExperiment{
		results:   &mockResult{},
		tableData: []map[string]any{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("code-rag-bench/mbpp"),
			huggingface.DatasetWithSamples(1000),
			huggingface.DatasetWithTextColumn("code"),
		),
	}

	experiment.prose = []projector.ProseEntry{
		{
			Condition: func() bool {
				return experiment.results.Score() > 0.5
			},
			Description: "The system is able to generate code for the language Python.",
		},
	}

	return experiment
}

func (experiment *LanguagesExperiment) Name() string {
	return "Languages"
}

func (experiment *LanguagesExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *LanguagesExperiment) Prompts() *vm.Loader {
	return tools.GetLoader(experiment.dataset, 1.0)
}

func (experiment *LanguagesExperiment) Holdout() (int, vm.HoldoutType) {
	return 50, vm.HoldoutLinear
}

/*
AddResult should emperically prove that the system generated the correct
code for the given prompt. It should compare the generated code with the
expected code and produce a score between 0 and 1.
*/
func (experiment *LanguagesExperiment) AddResult(res vm.SpanResult) {
	if experiment.Results == nil {
		experiment.Results = make([]vm.SpanResult, 0)
	}

	experiment.Results = append(experiment.Results, res)
}

/*
Outcome evaluates the overall result of the experiment, where we call a
failure if the total accuracy score is less than 0.5.
*/
func (experiment *LanguagesExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.5
}

func (experiment *LanguagesExperiment) Score() float64 {
	score := 0.0

	for _, res := range experiment.Results {
		score += res.Score
	}

	return score
}

func (experiment *LanguagesExperiment) TableData() []map[string]any {
	return experiment.tableData
}
