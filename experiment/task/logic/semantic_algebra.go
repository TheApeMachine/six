package logic

import (
	"fmt"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/store/data/provider/local"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

type SemanticAlgebraExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    []string
	evaluator *tools.Evaluator
}

func NewSemanticAlgebraExperiment() *SemanticAlgebraExperiment {
	// We load a generated dataset of logical facts to test GF(257) phase cancellation
	facts := []string{
		"Sandra is_in Garden",
		"Roy is_in Kitchen",
		"Cat sat_on Mat",
		"Bird flew_over Wall",
	}

	return &SemanticAlgebraExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   local.New(local.WithStrings(facts)),
		// Baseline 0.95: algebraic cancellation in GF(257) is exact.
		// If the stored phase is Roy·is_in·Kitchen and the query cancels
		// Roy·is_in, the residue must be Kitchen exactly. Partial credit
		// is meaningless for this task — it either works or it doesn't.
		// Target 1.0: perfect cancellation is the design goal.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.95, 1.0),
		),
	}
}

func (experiment *SemanticAlgebraExperiment) Name() string {
	return "holographic_algebra"
}

func (experiment *SemanticAlgebraExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *SemanticAlgebraExperiment) Prompts() []string {
	experiment.prompt = []string{}
	return experiment.prompt
}

func (experiment *SemanticAlgebraExperiment) Holdout() (int, input.HoldoutType) {
	return 0, input.RIGHT
}

func (experiment *SemanticAlgebraExperiment) Section() string {
	return "logic"
}

func (experiment *SemanticAlgebraExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *SemanticAlgebraExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *SemanticAlgebraExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *SemanticAlgebraExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *SemanticAlgebraExperiment) Artifacts() []tools.Artifact {
	n := len(experiment.tableData)
	score := experiment.Score()

	exactMatches := 0
	for _, row := range experiment.tableData {
		if row.Scores.Exact == 1.0 {
			exactMatches++
		}
	}

	exactRate := 0.0
	if n > 0 {
		exactRate = float64(exactMatches) / float64(n)
	}

	sampleLabels := make([]string, n)
	scoreLabels := []string{"Exact", "Partial", "Fuzzy", "Weighted"}
	heatData := make([][]any, 0, n*4)
	weightedVals := make([]float64, n)
	meanLine := make([]float64, n)

	for sIdx, row := range experiment.tableData {
		sampleLabels[sIdx] = fmt.Sprintf("S%d", sIdx+1)
		for cIdx, v := range []float64{row.Scores.Exact, row.Scores.Partial, row.Scores.Fuzzy, row.WeightedTotal} {
			heatData = append(heatData, []any{cIdx, sIdx, v})
		}

		weightedVals[sIdx] = row.WeightedTotal
		meanLine[sIdx] = score
	}

	panels := []tools.Panel{
		{
			Kind:        "heatmap",
			Title:       "Score Fingerprint",
			GridLeft:    "5%",
			GridRight:   "57%",
			GridTop:     "14%",
			GridBottom:  "18%",
			XLabels:     scoreLabels,
			XShow:       true,
			YLabels:     sampleLabels,
			YAxisName:   "Sample",
			HeatData:    heatData,
			HeatMin:     0,
			HeatMax:     1,
			ColorScheme: "viridis",
			ShowVM:      true,
			VMRight:     "43%",
		},
		{
			Kind:       "chart",
			Title:      "Weighted Score",
			GridLeft:   "58%",
			GridRight:  "4%",
			GridTop:    "14%",
			GridBottom: "18%",
			XLabels:    sampleLabels,
			XAxisName:  "Sample",
			XShow:      true,
			Series: []tools.PanelSeries{
				{Name: "Score", Kind: "bar", BarWidth: "55%", Data: weightedVals},
				{Name: fmt.Sprintf("Mean (%.2f)", score), Kind: "dashed", Symbol: "none", Color: "#f97316", Data: meanLine},
			},
			YMin: tools.Float64Ptr(0),
			YMax: tools.Float64Ptr(1),
		},
	}

	proseTemplate := `\subsection{Semantic Algebra --- GF(257) Fact Cancellation}
\label{sec:semantic_algebra}

\paragraph{Task Description.}
The semantic algebra experiment evaluates whether the substrate can perform
logical fact cancellation using arithmetic in GF(257).  A set of relational
facts (e.g., \texttt{Roy is\_in Kitchen}) is ingested. At test time the
query presents a partial fact (e.g., \texttt{Roy is\_in}) and the held-out
target is the missing entity (\texttt{Kitchen}).  The value representation
encodes each token as a GF(257) element; fact cancellation reduces to
modular subtraction, and the residue should uniquely identify the answer.

\paragraph{Results.}
Across $N = {{.N}}$ test samples the mean weighted score was {{.Score | f3}}.
Exact cancellation rate: {{.ExactRate | pct}}.

{{if ge .Score 0.95 -}}
\paragraph{Assessment.}
The substrate achieved near-perfect algebraic cancellation, confirming
that the GF(257) arithmetic path is functioning correctly.  This is the
expected result: the operation is exact by construction.
{{- else if ge .Score 0.5 -}}
\paragraph{Assessment.}
Partial cancellation was observed.  Some facts resolve correctly while
others produce residues that do not uniquely map to the expected entity.
This suggests value collision or boundary detection issues that need
investigation.
{{- else -}}
\paragraph{Assessment.}
Cancellation accuracy was low.  The GF(257) arithmetic path is not
producing correct residues at this stage, likely due to incomplete
integration of the phase encoding with the substrate retrieval path.
{{- end}}

Figure~\ref{fig:semantic_algebra_map} shows the trial outcome map.
`

	return []tools.Artifact{
		{
			Type:     tools.ArtifactMultiPanel,
			FileName: "semantic_algebra_map",
			Data: tools.MultiPanelData{
				Panels: panels,
				Width:  1100,
				Height: 420,
			},
			Title:   "Semantic Algebra — Trial Outcome Map",
			Caption: fmt.Sprintf("GF(257) fact cancellation. N=%d.", n),
			Label:   "fig:semantic_algebra_map",
		},
		{
			Type:     tools.ArtifactProse,
			FileName: "semantic_algebra_section.tex",
			Data: tools.ProseData{
				Template: proseTemplate,
				Data: map[string]any{
					"N":         n,
					"Score":     score,
					"ExactRate": exactRate,
				},
			},
		},
	}
}
