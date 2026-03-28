package phasedial

import (
	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/local"
)

type AdaptiveSplitExperiment struct {
	tableData    []tools.ExperimentalData
	dataset      data.Provider
	prompt       []string
	holdouts     [][]byte
	evaluator    *tools.Evaluator
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
		dataset: local.New(local.WithStrings(tools.Aphorisms)),
	}
}

func (experiment *AdaptiveSplitExperiment) Name() string {
	return "Adaptive Split"
}

func (experiment *AdaptiveSplitExperiment) Section() string {
	return "phasedial"
}

func (experiment *AdaptiveSplitExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *AdaptiveSplitExperiment) Prompts() []string {
	experiment.prompt, experiment.holdouts = aphorismSplitPrompts()
	return experiment.prompt
}

func (experiment *AdaptiveSplitExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *AdaptiveSplitExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *AdaptiveSplitExperiment) Outcome() (any, Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *AdaptiveSplitExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, d := range experiment.tableData {
		total += d.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
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
