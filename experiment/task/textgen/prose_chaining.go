package textgen

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/huggingface"
	"github.com/theapemachine/six/pkg/core/algo"
)

/*
ProseChainingExperiment evaluates multi-step predictive generation across a
real encyclopaedic corpus. Text from wikitext-103 (a large, high-quality
Wikipedia-derived dataset) is ingested; the substrate is then prompted with
the first 40% of each test sample. It must chain through the learned attractor
field to reconstruct the remaining 60%.

wikitext-103 was chosen over wikitext-2 for its substantially larger and more
diverse vocabulary, creating a denser coverage of the value attractor field.
This makes the chaining task harder and more informative: a favourable result
here implies the substrate generalises across the long tail of English prose,
not just the most frequent n-grams.
*/
type ProseChainingExperiment struct {
	tableData []tools.ExperimentalData
	dataset   data.Provider
	prompt    []string
	holdouts  [][]byte
	evaluator *tools.Evaluator
}

func NewProseChainingExperiment() *ProseChainingExperiment {
	return &ProseChainingExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("wikitext"),
			huggingface.DatasetWithSubset("wikitext-103-raw-v1"),
			huggingface.DatasetWithSamples(10),
			huggingface.DatasetWithTextColumn("text"),
		),
		// Baseline 0.03: with 60% holdout on diverse encyclopedia text,
		// even partial n-gram recovery is evidence of structural recall.
		// Target 0.35: aggressive holdout makes high scores hard.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.03, 0.35),
		),
	}
}

func (experiment *ProseChainingExperiment) Name() string           { return "Prose Chaining" }
func (experiment *ProseChainingExperiment) Section() string        { return "textgen" }
func (experiment *ProseChainingExperiment) Dataset() data.Provider { return experiment.dataset }

func (experiment *ProseChainingExperiment) Prompts() []string {
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
		prefix, hold := tools.BytePrefixFraction(p.Text, 0.4)
		if hold == "" {
			continue
		}
		experiment.prompt = append(experiment.prompt, prefix)
		experiment.holdouts = append(experiment.holdouts, []byte(hold))
	}
	return experiment.prompt
}

func (experiment *ProseChainingExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *ProseChainingExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *ProseChainingExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *ProseChainingExperiment) OutcomeForPrompt(idx int) (any, gc.Assertion, any) {
	return tools.EvaluatorOutcomeForPrompt(experiment.evaluator, experiment.tableData, idx)
}

func (experiment *ProseChainingExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *ProseChainingExperiment) TableData() any { return experiment.tableData }

/*
Answer returns the generated text continuation.
*/
func (experiment *ProseChainingExperiment) Answer(prediction *algo.Prediction) string {
	return prediction.String()
}

func (experiment *ProseChainingExperiment) Artifacts() []tools.Artifact {
	return ProseChainingArtifacts(experiment.tableData, experiment.Score())
}
