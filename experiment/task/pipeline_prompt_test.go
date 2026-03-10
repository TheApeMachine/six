package task

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
)

type PipelinePromptExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

func NewPipelinePromptExperiment() *PipelinePromptExperiment {
	full := "Mary moved to the bathroom. John went to the hallway. Where is Mary?bathroom"

	return &PipelinePromptExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   NewLocalProvider([]string{full}),
		prompt: tokenizer.NewPrompt(
			tokenizer.PromptWithSamples([]tokenizer.PromptSample{
				{
					Visible: "Mary moved to the bathroom. John went to the hallway. Where is Mary?",
					HeldOut: "bathroom",
					Full:    full,
				},
			}),
		),
	}
}

func (experiment *PipelinePromptExperiment) Name() string {
	return "Pipeline Prompt"
}

func (experiment *PipelinePromptExperiment) Section() string {
	return "logic"
}

func (experiment *PipelinePromptExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *PipelinePromptExperiment) Prompts() *tokenizer.Prompt {
	return experiment.prompt
}

func (experiment *PipelinePromptExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.RIGHT
}

func (experiment *PipelinePromptExperiment) AddResult(result tools.ExperimentalData) {
	result.Scores = tools.ByteScores(result.Holdout, result.Observed)
	result.WeightedTotal = tools.WeightedTotal(
		result.Scores.Exact,
		result.Scores.Partial,
		result.Scores.Fuzzy,
	)

	experiment.tableData = append(experiment.tableData, result)
}

func (experiment *PipelinePromptExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldEqual, 1.0
}

func (experiment *PipelinePromptExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *PipelinePromptExperiment) Artifacts() []tools.Artifact {
	return nil
}

func (experiment *PipelinePromptExperiment) Finalize(*geometry.HybridSubstrate) error {
	return nil
}

func (experiment *PipelinePromptExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}

	return experiment.tableData[0].WeightedTotal
}

func TestPipelinePromptUsesPromptContextReadout(t *testing.T) {
	gc.Convey("Given a pipeline experiment with an explicit visible prefix and held-out answer", t, func() {
		experiment := NewPipelinePromptExperiment()
		pipeline, err := NewPipeline(
			PipelineWithExperiment(experiment),
			PipelineWithReporter(NewSnapshotReporter()),
		)

		gc.So(err, gc.ShouldBeNil)
		gc.So(pipeline, gc.ShouldNotBeNil)

		gc.Convey("When the pipeline runs", func() {
			err = pipeline.Run()

			gc.So(err, gc.ShouldBeNil)
			gc.So(experiment.tableData, gc.ShouldHaveLength, 1)
			gc.So(string(experiment.tableData[0].Observed), gc.ShouldEqual, "bathroom")
			gc.So(experiment.tableData[0].Scores.Exact, gc.ShouldEqual, 1.0)
		})
	})
}
