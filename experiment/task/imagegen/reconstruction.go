package imagegen

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
ReconstructionExperiment tests the ability of the system to generate code for
various programming languages.
*/
type ReconstructionExperiment struct {
	tableData []map[string]any
	prose     []projector.ProseEntry
	results   []experiment.Result
	dataset   provider.Dataset
}

func NewReconstructionExperiment() *ReconstructionExperiment {
	experiment := &ReconstructionExperiment{
		tableData: []map[string]any{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("uoft-cs/cifar10"),
			huggingface.DatasetWithSamples(100),
			huggingface.DatasetWithTextColumn("img"),
		),

		results: make([]experiment.Result, 0),
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
	return "babi_benchmark"
}

func (experiment *ReconstructionExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *ReconstructionExperiment) Prompts() *vm.Loader {
	return tools.GetLoader(experiment.dataset, 1.0)
}

func (experiment *ReconstructionExperiment) Holdout() (int, vm.HoldoutType) {
	return 0, vm.HoldoutLinear
}

/*
AddResult should emperically prove that the system generated the correct
code for the given prompt. It should compare the generated code with the
expected code and produce a score between 0 and 1.
*/
func (experiment *ReconstructionExperiment) AddResult(results map[string]any) {
	// Compare the result to the prompt, and get a difference in percentage.
	prompt := results["prompt"].([]byte)
	res := results["result"].([]byte)
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
	total := 0.0

	for _, data := range experiment.tableData {
		scores := data["scores"].(map[string]float64)

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
