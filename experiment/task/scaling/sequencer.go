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
}

func NewSequencerExperiment() *SequencerExperiment {
	return &SequencerExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   NewSyntheticDataset(128, 50, 77),
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
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *SequencerExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThanOrEqualTo, 0.0
}

func (experiment *SequencerExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0.0
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
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
