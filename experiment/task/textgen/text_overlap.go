package textgen

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/store/data/provider/huggingface"
	config "github.com/theapemachine/six/pkg/system/core"
	"github.com/theapemachine/six/pkg/system/vm/input"
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
	prompt    []string
	evaluator *tools.Evaluator
}

func NewTextOverlapExperiment() *TextOverlapExperiment {
	return &TextOverlapExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("roneneldan/TinyStories"),
			huggingface.DatasetWithSamples(config.Experiment.Samples),
			huggingface.DatasetWithTextColumn("text"),
		),
		// Baseline 0.05: TinyStories overlap patterns are dense enough
		// that random chord hits should produce some partial bridging.
		// Target 0.55: heavy vocabulary reuse should allow strong
		// bridging once sufficient attractor density accumulates.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.55),
		),
	}
}

func (experiment *TextOverlapExperiment) Name() string              { return "Text Overlap" }
func (experiment *TextOverlapExperiment) Section() string           { return "textgen" }
func (experiment *TextOverlapExperiment) Dataset() provider.Dataset { return experiment.dataset }

func (experiment *TextOverlapExperiment) Prompts() []string {
	experiment.prompt = []string{}
	return experiment.prompt
}

// 40% right holdout — tests generation across the second main act of each story.
func (experiment *TextOverlapExperiment) Holdout() (int, input.HoldoutType) {
	return 40, input.RIGHT
}

func (experiment *TextOverlapExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *TextOverlapExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *TextOverlapExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *TextOverlapExperiment) TableData() any { return experiment.tableData }

func (experiment *TextOverlapExperiment) Artifacts() []tools.Artifact {
	return TextOverlapArtifacts(experiment.tableData, experiment.Score())
}
