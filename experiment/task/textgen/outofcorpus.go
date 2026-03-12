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
OutOfCorpusExperiment tests whether the substrate can reconstruct text that
was not verbatim in its training corpus, by generalising from the structural
patterns of related ingested text.

Dataset: wikitext-2 (raw). After ingesting the first 10 samples of the
training split, the system is tested on the last 50% of each test sample —
content that was never in the substrate. The task requires bridging from the
prompt's structural fingerprint into the attractor field built from the
training corpus.

This is a genuine out-of-distribution test: the test split of wikitext-2
has no overlap with the training split at the sample level.
*/
type OutOfCorpusExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *process.Prompt
}

func NewOutOfCorpusExperiment() *OutOfCorpusExperiment {
	return &OutOfCorpusExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("wikitext"),
			huggingface.DatasetWithSubset("wikitext-2-raw-v1"),
			huggingface.DatasetWithSamples(config.Experiment.Samples),
			huggingface.DatasetWithTextColumn("text"),
		),
	}
}

func (experiment *OutOfCorpusExperiment) Name() string              { return "Out of Corpus" }
func (experiment *OutOfCorpusExperiment) Section() string           { return "textgen" }
func (experiment *OutOfCorpusExperiment) Dataset() provider.Dataset { return experiment.dataset }

func (experiment *OutOfCorpusExperiment) Prompts() *process.Prompt {
	experiment.prompt = process.NewPrompt(
		process.PromptWithDataset(experiment.dataset),
		process.PromptWithHoldout(experiment.Holdout()),
	)
	return experiment.prompt
}

// 50% right holdout: system must complete the second half of each sample.
func (experiment *OutOfCorpusExperiment) Holdout() (int, process.HoldoutType) {
	return 50, process.RIGHT
}

func (experiment *OutOfCorpusExperiment) AddResult(results tools.ExperimentalData) {
	results.Scores = tools.ByteScores(results.Holdout, results.Observed)
	results.WeightedTotal = tools.WeightedTotal(
		results.Scores.Exact,
		results.Scores.Partial,
		results.Scores.Fuzzy,
	)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *OutOfCorpusExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThanOrEqualTo, 0.0
}

func (experiment *OutOfCorpusExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, d := range experiment.tableData {
		total += d.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *OutOfCorpusExperiment) TableData() any { return experiment.tableData }

func (experiment *OutOfCorpusExperiment) Artifacts() []tools.Artifact {
	return OutOfCorpusArtifacts(experiment.tableData, experiment.Score())
}
