package scaling

import (
	"fmt"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
)

/*
SequencerExperiment evaluates how boundary detection and retrieval quality
scale across corpus depth. Ingests 200 samples (128 B each) through the
Sequencer and issues 50 queries with 32-byte held-out suffixes drawn from
positions spread across the corpus. The scaling question: does retrieval
accuracy degrade for samples ingested later as the substrate fills?
*/
type SequencerExperiment struct {
	tableData []tools.ExperimentalData
	dataset   data.Provider
	prompt    []string
	holdouts  [][]byte
	evaluator *tools.Evaluator
}

func NewSequencerExperiment() *SequencerExperiment {
	return &SequencerExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   NewSyntheticDataset(128, 200, 77),
		evaluator: tools.NewEvaluator(
			tools.EvalWithScalingInstrumentScorer(),
			tools.EvalWithExpectation(0.05, 0.50),
		),
	}
}

func (experiment *SequencerExperiment) Name() string    { return "Sequencer" }
func (experiment *SequencerExperiment) Section() string { return "scaling" }
func (experiment *SequencerExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *SequencerExperiment) Prompts() []string {
	ds, ok := experiment.dataset.(*SyntheticDataset)
	if !ok {
		experiment.prompt = experiment.prompt[:0]
		experiment.holdouts = nil
		return experiment.prompt
	}

	experiment.prompt, experiment.holdouts = syntheticSamplePrompts(ds, 50, 32)
	return experiment.prompt
}

func (experiment *SequencerExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}

	return experiment.holdouts[idx], true
}

func (experiment *SequencerExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *SequencerExperiment) Outcome() (any, Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *SequencerExperiment) OutcomeForPrompt(idx int) (any, Assertion, any) {
	return tools.EvaluatorOutcomeForPrompt(experiment.evaluator, experiment.tableData, idx)
}

func (experiment *SequencerExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *SequencerExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *SequencerExperiment) Artifacts() []tools.Artifact {
	return SequencerArtifacts(experiment.tableData)
}

func (experiment *SequencerExperiment) RawOutput() bool { return false }

func (experiment *SequencerExperiment) Finalize(substrate any) error {
	entries := 0

	for _, row := range experiment.tableData {
		if len(row.Generation) > 0 {
			entries++
		}
	}

	if entries == 0 {
		entries = 1
	}

	experiment.AddResult(tools.ExperimentalData{
		Idx:  len(experiment.tableData),
		Name: fmt.Sprintf("Summary: %d substrate entries", entries),
		Scores: tools.Scores{
			Exact:   float64(entries),
			Partial: float64(entries),
		},
		WeightedTotal: experiment.Score(),
	})

	return nil
}

var _ tools.HoldoutProvider = (*SequencerExperiment)(nil)
