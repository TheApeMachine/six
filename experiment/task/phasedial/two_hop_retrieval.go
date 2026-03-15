package phasedial

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

type TwoHopRetrievalExperiment struct {
	tableData       []tools.ExperimentalData
	dataset         provider.Dataset
	prompt          []string
	evaluator *tools.Evaluator
	phases          []string
	simCA           []float64
	simCB           []float64
	gains           []float64
	xAxis           []string
	base1Data       []float64
	base2Data       []float64
	composedData    []float64
	summaryRows     []map[string]any
	overallBestGain float64
}

func NewTwoHopRetrievalExperiment() *TwoHopRetrievalExperiment {
	return &TwoHopRetrievalExperiment{
		tableData: []tools.ExperimentalData{},
		// Baseline 0.20: Two-hop compositional retrieval.
		// Any non-zero result demonstrates the property holds.
		// Target 0.60: strong geometric invariant.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.20, 0.60),
		),
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *TwoHopRetrievalExperiment) Name() string {
	return "Two-Hop Retrieval"
}

func (experiment *TwoHopRetrievalExperiment) Section() string {
	return "phasedial"
}

func (experiment *TwoHopRetrievalExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *TwoHopRetrievalExperiment) Prompts() []string {
	return []string{
		"Predict the secondary structure of the given amino acid sequence.",
	}
}

func (experiment *TwoHopRetrievalExperiment) Holdout() (int, input.HoldoutType) {
	return 0, input.RIGHT
}

func (experiment *TwoHopRetrievalExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *TwoHopRetrievalExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *TwoHopRetrievalExperiment) Score() float64 {
	return experiment.overallBestGain
}

func (experiment *TwoHopRetrievalExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *TwoHopRetrievalExperiment) RawOutput() bool { return false }
















func (experiment *TwoHopRetrievalExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactLineChart,
			FileName: "composition_trace",
			Data: tools.LineChartData{
				XAxis: experiment.phases,
				Series: []tools.LineSeries{
					{Name: "sim(C,A)", Data: experiment.simCA},
					{Name: "sim(C,B)", Data: experiment.simCB},
					{Name: "Gain min(CA,CB)", Data: experiment.gains},
				},
				YMin: -1.0,
				YMax: 1.0,
			},
			Title:   "Two-Hop Composition Trace",
			Caption: "Phase displacement sweep: sim(C,A), sim(C,B), and gain for composed midpoint.",
			Label:   "fig:composition_trace",
		},
		{
			Type:     tools.ArtifactBarChart,
			FileName: "two_hop_gain_by_alpha1",
			Data: tools.BarChartData{
				XAxis: experiment.xAxis,
				Series: []tools.BarSeries{
					{Name: "Base1", Data: experiment.base1Data},
					{Name: "Base2", Data: experiment.base2Data},
					{Name: "Composed", Data: experiment.composedData},
				},
			},
			Title:   "Two-Hop Gain by First-Hop Angle",
			Caption: "Baseline vs composed gain across first-hop angles.",
			Label:   "fig:two_hop_gain_bar",
		},
		{
			Type:     tools.ArtifactTable,
			FileName: "two_hop_summary.tex",
			Data:     experiment.summaryRows,
			Title:    "Two-Hop Summary",
			Caption:  "Best match and gains for two-hop composition.",
			Label:    "tab:two_hop_summary",
		},
	
{
Type:     tools.ArtifactProse,
FileName: "two_hop_retrieval_section.tex",
Data: tools.ProseData{
Template: `\subsection{Two-Hop Retrieval}
\label{sec:two_hop_retrieval}

\paragraph{Task Description.}
The two hop retrieval experiment evaluates compositional retrieval via two sequential phase-space hops.
Starting from seed entry A, the system composes A with a best-matching
entry B to form a composed fingerprint AB, then searches for a third
entry C that is simultaneously related to both A and B.  The gain
measures whether composition discovers entries unreachable by single-hop.

\paragraph{Results.}
Figure~\ref{fig:two_hop_retrieval_map} shows the trial outcome map.
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
