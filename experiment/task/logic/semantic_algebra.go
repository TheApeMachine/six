package logic

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/logic/semantic"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/store/data/provider/local"
	"github.com/theapemachine/six/pkg/system/process"
)

type SemanticAlgebraExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *process.Prompt
	engine    *semantic.Engine
}

func NewSemanticAlgebraExperiment() *SemanticAlgebraExperiment {
	// We load a generated dataset of logical facts to test GF(257) phase cancellation
	facts := []string{
		"Sandra is_in Garden",
		"Roy is_in Kitchen",
		"Cat sat_on Mat",
		"Bird flew_over Wall",
	}

	return &SemanticAlgebraExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   local.New(local.WithStrings(facts)),
		engine:    semantic.NewEngine(),
	}
}

func (experiment *SemanticAlgebraExperiment) Name() string {
	return "holographic_algebra"
}

func (experiment *SemanticAlgebraExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *SemanticAlgebraExperiment) Prompts() *process.Prompt {
	experiment.prompt = process.NewPrompt(
		process.PromptWithDataset(experiment.dataset),
	)
	return experiment.prompt
}

func (experiment *SemanticAlgebraExperiment) Holdout() (int, process.HoldoutType) {
	return 0, process.RIGHT
}

func (experiment *SemanticAlgebraExperiment) Section() string {
	return "logic"
}

func (experiment *SemanticAlgebraExperiment) AddResult(results tools.ExperimentalData) {
	// Evaluates the result mathematically using GF(257) logic
	results.Scores = tools.ByteScores(results.Holdout, results.Observed)

	results.WeightedTotal = tools.WeightedTotal(
		results.Scores.Exact,
		results.Scores.Partial,
		results.Scores.Fuzzy,
	)

	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *SemanticAlgebraExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThanOrEqualTo, 0.95
}

func (experiment *SemanticAlgebraExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}

	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}

	return total / float64(len(experiment.tableData))
}

func (experiment *SemanticAlgebraExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *SemanticAlgebraExperiment) Artifacts() []tools.Artifact {
	return nil
}
