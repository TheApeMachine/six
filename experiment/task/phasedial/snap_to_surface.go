package phasedial

import (
	"fmt"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/local"
	"github.com/theapemachine/six/experiment/projector"
)

/*
SnapToSurfaceExperiment evaluates the "snap-to-surface" mechanism, where a
composed midpoint in phase space is rotated to maximize its resonance with
the corpus manifold. This ensures that compositional results land on valid
structural nodes.
*/
type SnapToSurfaceExperiment struct {
	tableData []tools.ExperimentalData
	dataset   data.Provider
	prompt    []string
	holdouts  [][]byte
	evaluator *tools.Evaluator
}

func NewSnapToSurfaceExperiment() *SnapToSurfaceExperiment {
	return &SnapToSurfaceExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   local.New(local.WithStrings(tools.Aphorisms)),
		// Baseline 0.05: snap-to-surface geometric property.
		// Any non-zero result demonstrates the property holds.
		// Target 0.50: strong geometric invariant.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.50),
		),
	}
}

func (experiment *SnapToSurfaceExperiment) Name() string {
	return "Snap to Surface"
}

func (experiment *SnapToSurfaceExperiment) Section() string {
	return "phasedial"
}

func (experiment *SnapToSurfaceExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *SnapToSurfaceExperiment) Prompts() []string {
	experiment.prompt, experiment.holdouts = aphorismSplitPrompts()
	return experiment.prompt
}

func (experiment *SnapToSurfaceExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *SnapToSurfaceExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *SnapToSurfaceExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
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
	n := len(experiment.tableData)
	score := experiment.Score()
	return PhasedialSectionArtifacts(
		"Snap to Surface",
		experiment.tableData,
		score,
		tools.ExperimentSection{
			Title: "Snap to Surface",
			Label: "snap_to_surface",
			TaskDescription: `The snap-to-surface experiment evaluates whether a composed midpoint in
phase space can be rotated to maximize its resonance with the corpus
manifold. This ensures that compositional results land on valid
structural nodes rather than falling into interstitial regions between
attractors.

The substrate ingests a set of aphorisms; after two-hop composition the
resulting midpoint fingerprint is searched against the manifold surface.
The score reflects how accurately the nearest valid substrate entry is
recovered.`,
			Results:    fmt.Sprintf(`Across $N = %d$ test samples the mean weighted score was %s.`, n, projector.F3(score)),
			Assessment: phasedialAssessment(score),
			FigureRef:  "fig:snap_to_surface_map",
		},
	)
}
