package scaling

import (
	"fmt"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

/*
SequencerExperiment evaluates boundary detection and retrieval quality.
Provides a 1000-sample synthetic dataset. The Pipeline ingests through the
Sequencer and prompts with held-out suffixes. Score reflects retrieval accuracy
over all prompts.
*/
type SequencerExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    []string
	evaluator *tools.Evaluator
}

func NewSequencerExperiment() *SequencerExperiment {
	return &SequencerExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   NewSyntheticDataset(128, 50, 77),
		// Baseline 0.05: synthetic 128-byte samples with 32-byte holdout.
		// The sequencer must detect boundaries and route retrieval correctly.
		// Any partial byte recovery demonstrates boundary-aware indexing.
		// Target 0.50: strong structural retrieval on synthetic data.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.50),
		),
	}
}

func (experiment *SequencerExperiment) Name() string    { return "Sequencer" }
func (experiment *SequencerExperiment) Section() string { return "scaling" }
func (experiment *SequencerExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *SequencerExperiment) Prompts() []string {
	experiment.prompt = []string{}
	return experiment.prompt
}

func (experiment *SequencerExperiment) Holdout() (int, input.HoldoutType) {
	return 32, input.RIGHT
}

func (experiment *SequencerExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *SequencerExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
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
	entries := 1

	experiment.AddResult(tools.ExperimentalData{
		Idx:  len(experiment.tableData),
		Name: fmt.Sprintf("Summary: %d substrate entries", entries),
		Scores: tools.Scores{
			Exact:   float64(entries),
			Partial: experiment.Score(),
		},
		WeightedTotal: experiment.Score(),
	})

	return nil
}
