package phasedial

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

type AdaptiveSplitExperiment struct {
	tableData    []tools.ExperimentalData
	dataset      provider.Dataset
	prompt       []string
	evaluator *tools.Evaluator
	adaptGain    float64
	boundaryRows []map[string]any
	summaryRows  []map[string]any
	gapXAxis     []string
	gapGains     []float64
}

func NewAdaptiveSplitExperiment() *AdaptiveSplitExperiment {
	return &AdaptiveSplitExperiment{
		tableData: []tools.ExperimentalData{},
		// Baseline 0.05: Adaptive split geometric property.
		// Any non-zero result demonstrates the property holds.
		// Target 0.50: strong geometric invariant.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.50),
		),
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *AdaptiveSplitExperiment) Name() string {
	return "Adaptive Split"
}

func (experiment *AdaptiveSplitExperiment) Section() string {
	return "phasedial"
}

func (experiment *AdaptiveSplitExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *AdaptiveSplitExperiment) Prompts() []string {
	experiment.prompt = []string{}

	return experiment.prompt
}

func (experiment *AdaptiveSplitExperiment) Holdout() (int, input.HoldoutType) {
	return 0, input.RIGHT
}

func (experiment *AdaptiveSplitExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *AdaptiveSplitExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *AdaptiveSplitExperiment) Score() float64 {
	return experiment.adaptGain
}

func (experiment *AdaptiveSplitExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *AdaptiveSplitExperiment) RawOutput() bool { return false }





























func (experiment *AdaptiveSplitExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactTable,
			FileName: "adaptive_split_boundaries.tex",
			Data:     experiment.boundaryRows,
			Title:    "Adaptive Split Boundaries",
			Caption:  "Top 5 boundary candidates ranked by combined balance/decoherence.",
			Label:    "tab:adaptive_split_boundaries",
		},
		{
			Type:     tools.ArtifactBarChart,
			FileName: "adaptive_split_gap",
			Data: tools.BarChartData{
				XAxis:  experiment.gapXAxis,
				Series: []tools.BarSeries{{Name: "Best Gain", Data: experiment.gapGains}},
			},
			Title:   "Adaptive Split Gap Experiment",
			Caption: "Best gain for each gap size.",
			Label:   "fig:adaptive_split_gap",
		},
		{
			Type:     tools.ArtifactTable,
			FileName: "adaptive_split_summary.tex",
			Data:     experiment.summaryRows,
			Title:    "Adaptive Split Summary",
			Caption:  "Comparison of adaptive split vs reference split.",
			Label:    "tab:adaptive_split_summary",
		},
	
{
Type:     tools.ArtifactProse,
FileName: "adaptive_split_section.tex",
Data: tools.ProseData{
Template: `\subsection{Adaptive Split}
\label{sec:adaptive_split}

\paragraph{Task Description.}
The adaptive split experiment evaluates the optimal boundary in the PhaseDial for splitting
compositional fingerprints into independently steerable sub-manifolds.
It sweeps candidate boundaries through the residual field and selects
the split that maximises independent perspective shifts while maintaining
structural balance between left and right sub-fingerprints.

\paragraph{Results.}
Figure~\ref{fig:adaptive_split_map} shows the trial outcome map.
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
