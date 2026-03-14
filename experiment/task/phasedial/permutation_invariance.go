package phasedial

import (
	"math"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

/*
PermutationInvarianceExperiment evaluates whether the PhaseDial's retrieval
properties are invariant to the order of ingestion. It performs a geodesic
scan and generates a multi-panel chart showing the semantic geodesic matrix.
*/
type PermutationInvarianceExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    []string
}

func NewPermutationInvarianceExperiment() *PermutationInvarianceExperiment {
	return &PermutationInvarianceExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *PermutationInvarianceExperiment) Name() string {
	return "Permutation Invariance"
}

func (experiment *PermutationInvarianceExperiment) Section() string {
	return "phasedial"
}

func (experiment *PermutationInvarianceExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *PermutationInvarianceExperiment) Prompts() []string {
	return []string{
		"Predict the secondary structure of the given amino acid sequence.",
	}
}

func (experiment *PermutationInvarianceExperiment) Holdout() (int, input.HoldoutType) {
	return 0, input.RIGHT
}

func (experiment *PermutationInvarianceExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *PermutationInvarianceExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
}

func (experiment *PermutationInvarianceExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return math.NaN() // Not yet computed
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *PermutationInvarianceExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *PermutationInvarianceExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{}
}
