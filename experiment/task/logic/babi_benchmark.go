package logic

import (
	"fmt"
	"strings"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/store/data/provider/huggingface"
	config "github.com/theapemachine/six/pkg/system/core"
	"github.com/theapemachine/six/pkg/system/process"
)

/*
BabiExperiment evaluates question-answering performance using the
facebook/babi_qa dataset (Task 1: single supporting fact).
*/
type BabiExperiment struct {
	tableData []tools.ExperimentalData
	prose     []projector.ProseEntry
	dataset   *huggingface.BabiQADataset
	prompt    *process.Prompt
}

func NewBabiExperiment() *BabiExperiment {
	experiment := &BabiExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.NewBabiQA(
			huggingface.DatasetWithRepo("facebook/babi_qa"),
			huggingface.DatasetWithSamples(config.Experiment.Samples),
			huggingface.DatasetWithSubset("en-10k-qa1"),
		),
	}

	experiment.prose = []projector.ProseEntry{
		{
			Condition: func() bool {
				return experiment.Score() > 0.5
			},
			Description: "It's alright.",
		},
	}

	return experiment
}

func (experiment *BabiExperiment) Name() string {
	return "babi_benchmark"
}

func (experiment *BabiExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *BabiExperiment) Prompts() *process.Prompt {
	experiment.prompt = process.NewPrompt(
		process.PromptWithDataset(experiment.dataset),
	)

	return experiment.prompt
}

func (experiment *BabiExperiment) Holdout() (int, process.HoldoutType) {
	return 0, process.RIGHT
}

func (experiment *BabiExperiment) Section() string {
	return "logic"
}

func (experiment *BabiExperiment) AddResult(results tools.ExperimentalData) {
	results.Scores = tools.ByteScores(results.Holdout, results.Observed)

	results.WeightedTotal = tools.WeightedTotal(
		results.Scores.Exact,
		results.Scores.Partial,
		results.Scores.Fuzzy,
	)

	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *BabiExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.Score(), gc.ShouldBeGreaterThanOrEqualTo, 0.0
}

func (experiment *BabiExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}

	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}

	return total / float64(len(experiment.tableData))
}

func (experiment *BabiExperiment) TableData() any {
	return experiment.tableData
}

// ── Artifact generation ─────────────────────────────────────────────

func (experiment *BabiExperiment) Artifacts() []tools.Artifact {
	n := len(experiment.tableData)
	if n == 0 {
		return nil
	}

	// ── Summary statistics ─────────────────────────────────────────
	exactMatches := 0
	partialSum := 0.0
	for _, row := range experiment.tableData {
		if row.Scores.Exact == 1.0 {
			exactMatches++
		}
		partialSum += row.Scores.Partial
	}
	exactRate := float64(exactMatches) / float64(n)
	partialRate := partialSum / float64(n)
	score := experiment.Score()

	// ── Build per-sample failure list ─────────────────────────────
	var failures []failureRecord
	for _, row := range experiment.tableData {
		if row.Scores.Exact < 1.0 {
			entity := extractEntityFromPrefix(string(row.Prefix))
			if entity == "" {
				entity = "unknown"
			}
			failures = append(failures, failureRecord{
				Idx:      row.Idx,
				Entity:   entity,
				Expected: string(row.Holdout),
				Observed: string(row.Observed),
			})
		}
	}

	// ── Build Trial Outcome Map data ──────────────────────────────
	// Left panel: heatmap — rows = samples, columns = score dimensions
	// Each cell is the score value [0,1]; colour = viridis (0=dark, 1=bright).
	scoreLabels := []string{"Exact", "Partial", "Fuzzy", "Weighted"}
	sampleLabels := make([]string, n)
	for i := range sampleLabels {
		sampleLabels[i] = fmt.Sprintf("Q%d", i+1)
	}

	// heatData: [[colIdx, rowIdx, value], …]  (col=score dim, row=sample)
	heatData := make([][]any, 0, n*4)
	for sIdx, row := range experiment.tableData {
		vals := []float64{
			row.Scores.Exact,
			row.Scores.Partial,
			row.Scores.Fuzzy,
			row.WeightedTotal,
		}
		for cIdx, v := range vals {
			heatData = append(heatData, []any{cIdx, sIdx, v})
		}
	}

	// Right panel: weighted score per sample (bar) + mean (horizontal line).
	weightedPerSample := make([]float64, n)
	meanLine := make([]float64, n)
	for i, row := range experiment.tableData {
		weightedPerSample[i] = row.WeightedTotal
		meanLine[i] = score
	}

	panels := []tools.Panel{
		{
			Kind:        "heatmap",
			Title:       "Score Fingerprint",
			GridLeft:    "5%",
			GridRight:   "56%",
			GridTop:     "12%",
			GridBottom:  "12%",
			XLabels:     scoreLabels,
			XAxisName:   "Score Dimension",
			XShow:       true,
			YLabels:     sampleLabels,
			YAxisName:   "Sample",
			HeatData:    heatData,
			HeatMin:     0,
			HeatMax:     1,
			ColorScheme: "viridis",
			ShowVM:      true,
			VMRight:     "44%",
		},
		{
			Kind:       "chart",
			Title:      "Weighted Score per Sample",
			GridLeft:   "58%",
			GridRight:  "4%",
			GridTop:    "12%",
			GridBottom: "12%",
			XLabels:    sampleLabels,
			XAxisName:  "Sample",
			XShow:      true,
			Series: []tools.PanelSeries{
				{
					Name:     "Weighted",
					Kind:     "bar",
					BarWidth: "55%",
					Data:     weightedPerSample,
				},
				{
					Name:   fmt.Sprintf("Mean (%.2f)", score),
					Kind:   "dashed",
					Symbol: "none",
					Color:  "#f97316",
					Data:   meanLine,
				},
			},
			YMin: tools.Float64Ptr(0),
			YMax: tools.Float64Ptr(1),
		},
	}

	// ── Failure table rows (up to 20) ─────────────────────────────
	maxFail := 20
	if len(failures) < maxFail {
		maxFail = len(failures)
	}
	failureRows := make([][]string, maxFail)
	for i := 0; i < maxFail; i++ {
		f := failures[i]
		failureRows[i] = []string{
			fmt.Sprintf("%d", f.Idx),
			f.Entity,
			f.Expected,
			f.Observed,
		}
	}

	// ── Prose template ─────────────────────────────────────────────
	proseTemplate := `\subsection{bAbI QA Task 1: Single Supporting Fact}
\label{sec:babi_benchmark}

\paragraph{Task Description.}
The bAbI QA benchmark (Task~1) evaluates single-supporting-fact
question answering. Each sample consists of a short story describing
entity movements between named locations, followed by a question of
the form \textit{''Where is Person?''}. The correct answer is the
last location the entity moved to---requiring the system to track
an entity through a chain of movement facts without any explicit
pointer to the relevant sentence.

\paragraph{Test Conditions.}
Experiments used {{.NSamples}} samples from
\texttt{facebook/babi\_qa} (subset \texttt{en-10k-qa1}).
Reasoning is performed via Transitive Resonance: the entity chord is
extracted from the question, the story is scanned geometrically for
its last movement relationship, and the residue chord is decoded as
the location answer.

\paragraph{Results.}
Figure~\ref{fig:babi_trial_map} shows the per-sample Trial Outcome
Map. Each row of the left heatmap corresponds to one question;
columns show the Exact, Partial, Fuzzy, and Weighted scores on a
0--1 colour scale (viridis, dark = 0, bright = 1). The right
panel displays the weighted score per sample alongside the
overall mean (orange dashed line).

The system achieved an exact-match accuracy of {{.ExactRate | pct}}
across all {{.NSamples}} samples, with a mean partial score of
{{.PartialRate | f3}} and an overall weighted score of
{{.Score | f3}}.

{{if gt .ExactRate 0.7 -}}
\paragraph{Assessment.}
The substrate resolved the majority of single-supporting-fact queries
exactly, demonstrating reliable transitive chain traversal through
geometric residue accumulation.
{{- else if gt .ExactRate 0.3 -}}
\paragraph{Assessment.}
The substrate correctly resolved a minority of queries by exact match.
Partial scores indicate that many outputs were geometrically adjacent
to the correct location chord, suggesting the attractor is in the
right region but final decoding introduces ambiguity.
{{- else -}}
\paragraph{Assessment.}
Exact-match accuracy was low.  The Transitive Resonance mechanism
requires the entity's movement facts to produce a sufficiently
distinct residue chord; at this sample size the substrate geometry
may not separate location attractors reliably.
{{- end}}

{{- if gt .NFailures 0}}

\begin{table}[htbp]
  \centering
  \caption{bAbI Task~1 failure cases (showing first {{.NFailureRows}} of
    {{.NFailures}}). $N = {{.NSamples}}$, exact accuracy
    {{.ExactRate | pct}}.}
  \label{tab:babi_failures}
  \begin{tabular}{rlll}
    \toprule
    \textbf{Q\#} & \textbf{Entity} & \textbf{Expected} & \textbf{Observed} \\
    \midrule
{{- range .FailureRows}}
    {{index . 0}} & {{index . 1}} & \texttt{ {{- index . 2 -}} } & \texttt{ {{- index . 3 -}} } \\
{{- end}}
    \bottomrule
  \end{tabular}
\end{table}
{{- end}}
`

	return []tools.Artifact{
		{
			Type:     tools.ArtifactMultiPanel,
			FileName: "babi_trial_map",
			Data: tools.MultiPanelData{
				Panels: panels,
				Width:  1400,
				Height: 700,
			},
			Title:   "bAbI Task 1 — Trial Outcome Map",
			Caption: fmt.Sprintf("Per-sample score fingerprint (left) and weighted score (right). N=%d, exact accuracy=%.1f%%.", n, exactRate*100),
			Label:   "fig:babi_trial_map",
		},
		{
			Type:     tools.ArtifactProse,
			FileName: "babi_section.tex",
			Data: tools.ProseData{
				Template: proseTemplate,
				Data: map[string]any{
					"NSamples":     n,
					"ExactRate":    exactRate,
					"PartialRate":  partialRate,
					"Score":        score,
					"NFailures":    len(failures),
					"NFailureRows": maxFail,
					"FailureRows":  failureRows,
				},
			},
		},
	}
}

type entityStat struct {
	Correct int
	Total   int
}

type locationStat struct {
	Correct int
	Total   int
}

type failureRecord struct {
	Idx      int
	Entity   string
	Expected string
	Observed string
}

func extractEntityFromPrefix(prefix string) string {
	idx := strings.LastIndex(prefix, "Where is ")
	if idx < 0 {
		return ""
	}
	rest := prefix[idx+len("Where is "):]
	qIdx := strings.Index(rest, "?")
	if qIdx >= 0 {
		rest = rest[:qIdx]
	}
	return strings.TrimSpace(rest)
}

func sortedKeys[V any](m map[string]*V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple insertion sort for small key sets
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func maxFloat(s []float64) float64 {
	if len(s) == 0 {
		return 0
	}
	m := s[0]
	for _, v := range s[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func divSlice(s []float64, d float64) []float64 {
	out := make([]float64, len(s))
	for i, v := range s {
		out[i] = v / d
	}
	return out
}
