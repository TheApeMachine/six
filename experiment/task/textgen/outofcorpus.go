package textgen

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/huggingface"
	"github.com/theapemachine/six/pkg/core/algo"
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
	dataset   data.Provider
	prompt    []string
	holdouts  [][]byte
	evaluator *tools.Evaluator
}

func NewOutOfCorpusExperiment() *OutOfCorpusExperiment {
	return &OutOfCorpusExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("wikitext"),
			huggingface.DatasetWithSubset("wikitext-2-raw-v1"),
			huggingface.DatasetWithSamples(10),
			huggingface.DatasetWithTextColumn("text"),
		),
		// Baseline 0.05: any partial byte overlap with the held-out suffix
		// is evidence that the substrate encodes structural regularity.
		// Target 0.40: strong OOD generalization from byte patterns.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.40),
		),
	}
}

func (experiment *OutOfCorpusExperiment) Name() string           { return "Out of Corpus" }
func (experiment *OutOfCorpusExperiment) Section() string        { return "textgen" }
func (experiment *OutOfCorpusExperiment) Dataset() data.Provider { return experiment.dataset }

func (experiment *OutOfCorpusExperiment) Prompts() []string {
	experiment.prompt = experiment.prompt[:0]
	experiment.holdouts = experiment.holdouts[:0]
	pp, ok := experiment.dataset.(data.PromptProvider)
	if !ok {
		return experiment.prompt
	}
	for p := range pp.GeneratePrompts() {
		if len(p.Text) < 8 {
			continue
		}
		prefix, hold := tools.BytePrefixFraction(p.Text, 0.5)
		if hold == "" {
			continue
		}
		experiment.prompt = append(experiment.prompt, prefix)
		experiment.holdouts = append(experiment.holdouts, []byte(hold))
	}
	return experiment.prompt
}

func (experiment *OutOfCorpusExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *OutOfCorpusExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *OutOfCorpusExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *OutOfCorpusExperiment) OutcomeForPrompt(idx int) (any, gc.Assertion, any) {
	return tools.EvaluatorOutcomeForPrompt(experiment.evaluator, experiment.tableData, idx)
}

func (experiment *OutOfCorpusExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *OutOfCorpusExperiment) TableData() any { return experiment.tableData }

/*
Answer returns the generated text continuation.
*/
func (experiment *OutOfCorpusExperiment) Answer(prediction *algo.Prediction) string {
	return prediction.String()
}

func (experiment *OutOfCorpusExperiment) Artifacts() []tools.Artifact {
	return OutOfCorpusArtifacts(experiment.tableData, experiment.Score())
}
