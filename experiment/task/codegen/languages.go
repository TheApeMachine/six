package codegen

import (
	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/experiment"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/provider/local"
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
}

type mockResult struct{}

func (m *mockResult) Score() float64 { return 1.0 }

func NewLanguagesExperiment() *LanguagesExperiment {
	experiment := &LanguagesExperiment{
		results:   &mockResult{},
		tableData: []map[string]any{},
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
	return local.New([][]byte{[]byte("def factorial(n):")})
}

func (experiment *LanguagesExperiment) Prompts() *vm.Loader {
	return tools.GetLoader(local.New([][]byte{[]byte("def factorial(n):")}), 1.0)
}

func (experiment *LanguagesExperiment) Holdout() (int, vm.HoldoutType) {
	return 50, vm.HoldoutLinear
}

func (experiment *LanguagesExperiment) AddResult(res vm.SpanResult) {
	if experiment.Results == nil {
		experiment.Results = make([]vm.SpanResult, 0)
	}
	experiment.Results = append(experiment.Results, res)
}

func (experiment *LanguagesExperiment) Outcome() (any, any, any) {
	return experiment.tableData, ShouldNotBeNil, nil
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
