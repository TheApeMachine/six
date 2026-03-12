package textgen

import (
	gc "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/core"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/process"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/provider/huggingface"
)

/*
ProseChainingExperiment evaluates multi-step predictive generation across a
real encyclopaedic corpus. Text from wikitext-103 (a large, high-quality
Wikipedia-derived dataset) is ingested; the substrate is then prompted with
the first 40% of each test sample. It must chain through the learned attractor
field to reconstruct the remaining 60%.

wikitext-103 was chosen over wikitext-2 for its substantially larger and more
diverse vocabulary, creating a denser coverage of the chord attractor field.
This makes the chaining task harder and more informative: a favourable result
here implies the substrate generalises across the long tail of English prose,
not just the most frequent n-grams.
*/
type ProseChainingExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *process.Prompt
}

func NewProseChainingExperiment() *ProseChainingExperiment {
	return &ProseChainingExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("wikitext"),
			huggingface.DatasetWithSubset("wikitext-103-raw-v1"),
			huggingface.DatasetWithSamples(config.Experiment.Samples),
			huggingface.DatasetWithTextColumn("text"),
		),
	}
}

func (experiment *ProseChainingExperiment) Name() string              { return "Prose Chaining" }
func (experiment *ProseChainingExperiment) Section() string           { return "textgen" }
func (experiment *ProseChainingExperiment) Dataset() provider.Dataset { return experiment.dataset }

func (experiment *ProseChainingExperiment) Prompts() *process.Prompt {
	experiment.prompt = process.NewPrompt(
		process.PromptWithDataset(experiment.dataset),
		process.PromptWithHoldout(experiment.Holdout()),
	)
	return experiment.prompt
}

// 60% right holdout — an aggressive masking that tests deep generative chaining.
func (experiment *ProseChainingExperiment) Holdout() (int, process.HoldoutType) {
	return 60, process.RIGHT
}

func (experiment *ProseChainingExperiment) AddResult(results tools.ExperimentalData) {
	results.Scores = tools.ByteScores(results.Holdout, results.Observed)
	results.WeightedTotal = tools.WeightedTotal(
		results.Scores.Exact,
		results.Scores.Partial,
		results.Scores.Fuzzy,
	)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *ProseChainingExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThanOrEqualTo, 0.0
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

func (experiment *ProseChainingExperiment) TableData() any { return experiment.tableData }

func (experiment *ProseChainingExperiment) Artifacts() []tools.Artifact {
	return ProseChainingArtifacts(experiment.tableData, experiment.Score())
}
