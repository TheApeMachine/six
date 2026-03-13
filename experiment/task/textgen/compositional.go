package textgen

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/store/data/provider/huggingface"
	config "github.com/theapemachine/six/pkg/system/core"
	"github.com/theapemachine/six/pkg/system/process"
)

/*
CompositionalExperiment evaluates the substrate's ability to recall and
recombine structural patterns across a real story corpus. TinyStories provides
short English stories with highly regular grammar patterns ("Once upon a time
there was a [adj] [noun] who liked to [verb]..."). After ingesting multiple
stories, the system is prompted with a 70% prefix of novel samples; it must
complete the held-out 30% by chord resonance across learned story patterns.

This tests compositional recall: can the attractor field reconstruct the
ending of a story whose opening shares structural motifs with ingested stories,
even when the specific nouns and events are novel?
*/
type CompositionalExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *process.Prompt
}

func NewCompositionalExperiment() *CompositionalExperiment {
	return &CompositionalExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("roneneldan/TinyStories"),
			huggingface.DatasetWithSamples(config.Experiment.Samples),
			huggingface.DatasetWithTextColumn("text"),
		),
	}
}

func (experiment *CompositionalExperiment) Name() string              { return "Compositional" }
func (experiment *CompositionalExperiment) Section() string           { return "textgen" }
func (experiment *CompositionalExperiment) Dataset() provider.Dataset { return experiment.dataset }

func (experiment *CompositionalExperiment) Prompts() *process.Prompt {
	experiment.prompt = process.NewPrompt(
		process.PromptWithDataset(experiment.dataset),
		process.PromptWithHoldout(experiment.Holdout()),
	)
	return experiment.prompt
}

// 30% right holdout: system must reconstruct the ending of each story.
func (experiment *CompositionalExperiment) Holdout() (int, process.HoldoutType) {
	return 30, process.RIGHT
}

func (experiment *CompositionalExperiment) AddResult(results tools.ExperimentalData) {
	results.Scores = tools.ByteScores(results.Holdout, results.Observed)
	results.WeightedTotal = tools.WeightedTotal(
		results.Scores.Exact,
		results.Scores.Partial,
		results.Scores.Fuzzy,
	)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *CompositionalExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThanOrEqualTo, 0.0
}

func (experiment *CompositionalExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, d := range experiment.tableData {
		total += d.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *CompositionalExperiment) TableData() any { return experiment.tableData }

func (experiment *CompositionalExperiment) Artifacts() []tools.Artifact {
	return CompositionalArtifacts(experiment.tableData, experiment.Score())
}
