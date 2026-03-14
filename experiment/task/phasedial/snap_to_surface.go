package phasedial

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

/*
SnapToSurfaceExperiment evaluates the "snap-to-surface" mechanism, where a
composed midpoint in phase space is rotated to maximize its resonance with
the corpus manifold. This ensures that compositional results land on valid
structural nodes.
*/
type SnapToSurfaceExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    []string
}

func NewSnapToSurfaceExperiment() *SnapToSurfaceExperiment {
	return &SnapToSurfaceExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *SnapToSurfaceExperiment) Name() string {
	return "Snap to Surface"
}

func (experiment *SnapToSurfaceExperiment) Section() string {
	return "phasedial"
}

func (experiment *SnapToSurfaceExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *SnapToSurfaceExperiment) Prompts() []string {
	return []string{
		"Predict the secondary structure of the given amino acid sequence.",
	}
}

func (experiment *SnapToSurfaceExperiment) Holdout() (int, input.HoldoutType) {
	return 0, input.RIGHT
}

func (experiment *SnapToSurfaceExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *SnapToSurfaceExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
}

func (experiment *SnapToSurfaceExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0.0 // No data yet
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *SnapToSurfaceExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *SnapToSurfaceExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{}
}
