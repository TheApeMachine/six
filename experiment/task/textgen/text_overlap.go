package textgen

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/huggingface"
)

var _ tools.HoldoutProvider = (*TextOverlapExperiment)(nil)

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
overlapping value patterns between the prompt boundary and a learned sequence.

TinyStories is intentionally chosen here (rather than Wikipedia) because its
controlled vocabulary makes the overlap phenomenon measurable: stories reuse
the same canonical verbs, settings, and character archetypes, creating
a denser web of value attractor bridges than raw encyclopaedic text.
*/
type TextOverlapExperiment struct {
	tableData []tools.ExperimentalData
	dataset   data.Provider
	prompt    []string
	holdouts  [][]byte
	evaluator *tools.Evaluator
}

func NewTextOverlapExperiment() *TextOverlapExperiment {
	return &TextOverlapExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.New(
			huggingface.DatasetWithRepo("roneneldan/TinyStories"),
			huggingface.DatasetWithSamples(100),
			huggingface.DatasetWithTextColumn("text"),
		),
		// Baseline 0.05: TinyStories overlap patterns are dense enough
		// that random value hits should produce some partial bridging.
		// Target 0.55: heavy vocabulary reuse should allow strong
		// bridging once sufficient attractor density accumulates.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.55),
		),
	}
}

func (experiment *TextOverlapExperiment) Name() string           { return "Text Overlap" }
func (experiment *TextOverlapExperiment) Section() string        { return "textgen" }
func (experiment *TextOverlapExperiment) Dataset() data.Provider { return experiment.dataset }

func (experiment *TextOverlapExperiment) Prompts() []string {
	experiment.prompt = experiment.prompt[:0]
	experiment.holdouts = experiment.holdouts[:0]
	for sample := range experiment.dataset.Generate() {
		task := string(sample.TaskPrompt())
		if len(task) < 8 {
			continue
		}

		// 40% right holdout → 60% left prefix.
		prefix, hold := tools.BytePrefixFraction(task, 0.6)
		if hold == "" {
			continue
		}

		experiment.prompt = append(experiment.prompt, prefix)
		experiment.holdouts = append(experiment.holdouts, []byte(hold))
	}

	return experiment.prompt
}

func (experiment *TextOverlapExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *TextOverlapExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *TextOverlapExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *TextOverlapExperiment) OutcomeForPrompt(idx int) (any, gc.Assertion, any) {
	return experiment.evaluator.OutcomeForPromptConvey(experiment.tableData, idx)
}

func (experiment *TextOverlapExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *TextOverlapExperiment) TableData() any { return experiment.tableData }

func (experiment *TextOverlapExperiment) Artifacts() []tools.Artifact {
	return TextOverlapArtifacts(experiment.tableData, experiment.Score())
}
