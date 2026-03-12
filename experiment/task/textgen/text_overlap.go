package textgen

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	config "github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/process"
	"github.com/theapemachine/six/pkg/provider"
	"github.com/theapemachine/six/pkg/provider/huggingface"
)

/*
TextOverlapExperiment evaluates overlap-aware span bridging using a real
narrative corpus. TinyStories provides short stories with highly regular
sentence structure and vocabulary repetition — ideal for testing whether the
substrate detects shared structural boundaries between ingested story spans
and novel test prompts.

The experiment ingests 100 TinyStories samples, then tests 40% right holdout
on novel samples to see if the boundary detection logic latches onto
the task of generating a continuation that bridges smoothly into
an adjacent corpus region, exploiting the substrate's ability to detect the
overlapping chord patterns between the prompt boundary and a learned sequence.

TinyStories is intentionally chosen here (rather than Wikipedia) because its
controlled vocabulary makes the overlap phenomenon measurable: stories reuse
the same canonical verbs, settings, and character archetypes, creating
a denser web of chord attractor bridges than raw encyclopaedic text.
*/
type TextOverlapExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *process.Prompt
}

func NewTextOverlapExperiment() *TextOverlapExperiment {
	return &TextOverlapExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("roneneldan/TinyStories"),
			huggingface.DatasetWithSamples(config.Experiment.Samples),
			huggingface.DatasetWithTextColumn("text"),
		),
	}
}

func (experiment *TextOverlapExperiment) Name() string              { return "Text Overlap" }
func (experiment *TextOverlapExperiment) Section() string           { return "textgen" }
func (experiment *TextOverlapExperiment) Dataset() provider.Dataset { return experiment.dataset }

func (experiment *TextOverlapExperiment) Prompts() *process.Prompt {
	experiment.prompt = process.NewPrompt(
		process.PromptWithDataset(experiment.dataset),
		process.PromptWithHoldout(experiment.Holdout()),
	)
	return experiment.prompt
}

// 40% right holdout — tests generation across the second main act of each story.
func (experiment *TextOverlapExperiment) Holdout() (int, process.HoldoutType) {
	return 40, process.RIGHT
}

func (experiment *TextOverlapExperiment) AddResult(results tools.ExperimentalData) {
	results.Scores = tools.ByteScores(results.Holdout, results.Observed)
	results.WeightedTotal = tools.WeightedTotal(
		results.Scores.Exact,
		results.Scores.Partial,
		results.Scores.Fuzzy,
	)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *TextOverlapExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThanOrEqualTo, 0.0
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

func (experiment *TextOverlapExperiment) TableData() any { return experiment.tableData }

func (experiment *TextOverlapExperiment) Artifacts() []tools.Artifact {
	return TextOverlapArtifacts(experiment.tableData, experiment.Score())
}
