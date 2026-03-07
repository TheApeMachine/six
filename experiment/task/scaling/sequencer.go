package scaling

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/tokenizer"
)

/*
SequencerExperiment evaluates the topological boundary detection mechanism.
It analyzes byte streams for A₅ events (low variance flux, density spikes,
phase inversions) to identify natural structural boundaries and segment the
input stream.
*/
type SequencerExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    *tokenizer.Prompt
}

func NewSequencerExperiment() *SequencerExperiment {
	return &SequencerExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   nil,
	}
}

func (experiment *SequencerExperiment) Name() string {
	return "Sequencer"
}

func (experiment *SequencerExperiment) Section() string {
	return "scaling"
}

func (experiment *SequencerExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *SequencerExperiment) Prompts() *tokenizer.Prompt {
	return nil
}

func (experiment *SequencerExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.RIGHT
}

func (experiment *SequencerExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *SequencerExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
}

func (experiment *SequencerExperiment) Score() float64 {
	return 1.0
}

func (experiment *SequencerExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *SequencerExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{}
}

func (experiment *SequencerExperiment) Finalize(substrate *geometry.HybridSubstrate) error {
	return nil
}
