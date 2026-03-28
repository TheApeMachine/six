package textgen

import (
	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/huggingface"
)

/*
CompositionalExperiment evaluates the substrate's ability to recall and
recombine structural patterns across a real story corpus. TinyStories provides
short English stories with highly regular grammar patterns ("Once upon a time
there was a [adj] [noun] who liked to [verb]..."). After ingesting multiple
stories, the system is prompted with a 70% prefix of novel samples; it must
complete the held-out 30% by value resonance across learned story patterns.

This tests compositional recall: can the attractor field reconstruct the
ending of a story whose opening shares structural motifs with ingested stories,
even when the specific nouns and events are novel?
*/
type CompositionalExperiment struct {
	tableData []tools.ExperimentalData
	dataset   data.Provider
	prompt    []string
	holdouts  [][]byte
	evaluator *tools.Evaluator
}

func NewCompositionalExperiment() *CompositionalExperiment {
	return &CompositionalExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("roneneldan/TinyStories"),
			huggingface.DatasetWithSamples(1000),
			huggingface.DatasetWithTextColumn("text"),
		),
		// Baseline 0.05: TinyStories has high structural regularity,
		// so even minimal attractor density should produce partial matches.
		// Target 0.60: the controlled vocabulary makes high scores
		// realistic once the substrate has enough story density.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.60),
		),
	}
}

func (experiment *CompositionalExperiment) Name() string           { return "Compositional" }
func (experiment *CompositionalExperiment) Section() string        { return "textgen" }
func (experiment *CompositionalExperiment) Dataset() data.Provider { return experiment.dataset }

func (experiment *CompositionalExperiment) Prompts() []string {
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
		prefix, hold := tools.BytePrefixFraction(p.Text, 0.7)
		if hold == "" {
			continue
		}
		experiment.prompt = append(experiment.prompt, prefix)
		experiment.holdouts = append(experiment.holdouts, []byte(hold))
	}
	return experiment.prompt
}

func (experiment *CompositionalExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

// 30% right holdout: system must reconstruct the ending of each story.
func (experiment *CompositionalExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *CompositionalExperiment) Outcome() (any, Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *CompositionalExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *CompositionalExperiment) TableData() any { return experiment.tableData }

func (experiment *CompositionalExperiment) Artifacts() []tools.Artifact {
	return CompositionalArtifacts(experiment.tableData, experiment.Score())
}
