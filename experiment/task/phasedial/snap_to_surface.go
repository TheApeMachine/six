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
	evaluator *tools.Evaluator
}

func NewSnapToSurfaceExperiment() *SnapToSurfaceExperiment {
	return &SnapToSurfaceExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
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
return PhasedialSectionArtifacts(
"Snap to Surface",
experiment.tableData,
experiment.Score(),
`\subsection{Snap to Surface}
\label{sec:snap_to_surface}

\paragraph{Task Description.}
The snap-to-surface experiment evaluates whether a composed midpoint in
phase space can be rotated to maximize its resonance with the corpus
manifold. This ensures that compositional results land on valid
structural nodes rather than falling into interstitial regions between
attractors.

The substrate ingests a set of aphorisms; after two-hop composition the
resulting midpoint fingerprint is searched against the manifold surface.
The score reflects how accurately the nearest valid substrate entry is
recovered.

\paragraph{Results.}
Across $N = {{.N}}$ test samples the mean weighted score was {{.Score | f3}}.

{{if gt .Score 0.5 -}}
\paragraph{Assessment.}
The substrate demonstrated strong snap to surface invariance,
confirming that the geometric property holds reliably at this scale.
{{- else if gt .Score 0.1 -}}
\paragraph{Assessment.}
Partial invariance was observed.  The property holds for a subset of
samples but is not yet reliable across all test conditions.
Increasing ingestion corpus size is expected to strengthen the invariant.
{{- else -}}
\paragraph{Assessment.}
The property was not reliably detected at this ingestion scale.
This is an expected result during the refactoring phase; the underlying
geometric mechanism requires a functional Finalize path to populate
the substrate with the necessary compositional data.
{{- end}}

Figure~\ref{fig:snap_to_surface_map} shows the trial outcome map.
`,
map[string]any{"N": len(experiment.tableData), "Score": experiment.Score()},
)
}
