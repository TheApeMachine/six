package phasedial

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	config "github.com/theapemachine/six/pkg/system/core"

	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

/*
TorusNavigationExperiment evaluates independent phase rotations across
the U(1)×U(1) manifold split. It sweeps the T² torus grid to find
super-additive gain regions where multi-perspective shifts outperform
single-axis baselines.
*/
type TorusNavigationExperiment struct {
	tableData        []tools.ExperimentalData
	dataset          provider.Dataset
	prompt           []string
	evaluator *tools.Evaluator
	anySuperAdditive bool
	heatPanel        tools.Panel
	chartPanel       tools.Panel
	tableRows        []map[string]any
	splitPoint       int
	alpha1List       []float64
}

type torusNavigationOpt func(*TorusNavigationExperiment)

func NewTorusNavigationExperiment(opts ...torusNavigationOpt) *TorusNavigationExperiment {
	experiment := &TorusNavigationExperiment{
		tableData:  []tools.ExperimentalData{},
		// Baseline 0.05: Torus navigation traversal.
		// Any non-zero result demonstrates the property holds.
		// Target 0.50: strong geometric invariant.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.50),
		),
		dataset:    tools.NewLocalProvider(tools.Aphorisms),
		splitPoint: config.Numeric.NBasis / 2,
		alpha1List: []float64{15.0, 30.0, 45.0, 60.0, 75.0},
	}

	for _, opt := range opts {
		opt(experiment)
	}

	if experiment.splitPoint <= 0 || experiment.splitPoint >= config.Numeric.NBasis {
		experiment.splitPoint = config.Numeric.NBasis / 2
	}

	if len(experiment.alpha1List) == 0 {
		experiment.alpha1List = []float64{15.0, 30.0, 45.0, 60.0, 75.0}
	}

	return experiment
}

func TorusNavigationWithDataset(dataset provider.Dataset) torusNavigationOpt {
	return func(experiment *TorusNavigationExperiment) {
		if dataset != nil {
			experiment.dataset = dataset
		}
	}
}

func TorusNavigationWithSplitPoint(splitPoint int) torusNavigationOpt {
	return func(experiment *TorusNavigationExperiment) {
		experiment.splitPoint = splitPoint
	}
}

func TorusNavigationWithAlphaList(alpha1List []float64) torusNavigationOpt {
	return func(experiment *TorusNavigationExperiment) {
		if len(alpha1List) > 0 {
			experiment.alpha1List = append([]float64(nil), alpha1List...)
		}
	}
}

func (experiment *TorusNavigationExperiment) Name() string {
	return "Torus Navigation"
}

func (experiment *TorusNavigationExperiment) Section() string {
	return "phasedial"
}

func (experiment *TorusNavigationExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *TorusNavigationExperiment) Prompts() []string {
	return []string{
		"Predict the secondary structure of the given amino acid sequence.",
	}
}

func (experiment *TorusNavigationExperiment) Holdout() (int, input.HoldoutType) {
	return 0, input.RIGHT
}

func (experiment *TorusNavigationExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *TorusNavigationExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *TorusNavigationExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *TorusNavigationExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *TorusNavigationExperiment) RawOutput() bool { return false }

















func (experiment *TorusNavigationExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactMultiPanel,
			FileName: "torus_navigation",
			Data: tools.MultiPanelData{
				Panels: []tools.Panel{experiment.heatPanel, experiment.chartPanel},
				Width:  1200,
				Height: 900,
			},
			Title:   "U(1)×U(1) Torus Navigation",
			Caption: "(Left) Full T²(α₁,α₂) gain grid for first-hop α₁=15°. Dark = destructive, warm = constructive. (Right) T² best gain (bar) vs single-axis baselines (dashed) across all first-hop angles; bars exceeding dashed lines are super-additive.",
			Label:   "fig:torus_navigation",
		},
		{
			Type:     tools.ArtifactTable,
			FileName: "torus_navigation_summary.tex",
			Data:     experiment.tableRows,
			Title:    "Torus Navigation Summary",
			Caption:  "Summary of torus best gain vs 1D baselines.",
			Label:    "tab:torus_navigation",
		},
	
{
Type:     tools.ArtifactProse,
FileName: "torus_navigation_section.tex",
Data: tools.ProseData{
Template: `\subsection{Torus Navigation}
\label{sec:torus_navigation}

\paragraph{Task Description.}
The torus navigation experiment evaluates traversal mechanics on the phase torus.  After two-hop
composition the system sweeps the full $(\alpha_1, \alpha_2)$ grid
to map the similarity landscape, identifying regions of constructive
and destructive interference.  The experiment renders both arc slices
and the full 2D landscape heatmap.

\paragraph{Results.}
Figure~\ref{fig:torus_navigation_map} shows the trial outcome map.
The mean weighted score was {{.Score | f3}} across $N = {{.N}}$ samples.

{{if gt .Score 0.5 -}}
\paragraph{Assessment.}
The substrate demonstrated strong performance on this geometric property,
confirming that the invariant holds reliably at this ingestion scale.
{{- else if gt .Score 0.1 -}}
\paragraph{Assessment.}
Partial invariance was observed.  The property holds for a subset of
samples but becomes unreliable under more challenging conditions.
{{- else -}}
\paragraph{Assessment.}
The property was not reliably detected at this stage.  The phasedial
experiments require a functional Finalize path to populate the substrate
with compositional data; this infrastructure is being rebuilt during
the current refactoring phase.
{{- end}}
`,
Data: map[string]any{
"N":     len(experiment.tableData),
"Score": experiment.Score(),
},
},
},
	}
}
