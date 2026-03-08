package logic

import (
	"fmt"
	"math"
	"strings"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/provider/huggingface"
	"github.com/theapemachine/six/tokenizer"
)

/*
BabiExperiment evaluates question-answering performance using the
facebook/babi_qa dataset (Task 1: single supporting fact).
*/
type BabiExperiment struct {
	tableData []tools.ExperimentalData
	prose     []projector.ProseEntry
	dataset   *huggingface.BabiQADataset
	prompt    *tokenizer.Prompt
}

func NewBabiExperiment() *BabiExperiment {
	experiment := &BabiExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.NewBabiQA(
			huggingface.DatasetWithRepo("facebook/babi_qa"),
			huggingface.DatasetWithSamples(10000),
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

func (experiment *BabiExperiment) Prompts() *tokenizer.Prompt {
	samples, err := experiment.dataset.Samples()
	if err != nil {
		return tokenizer.NewPrompt()
	}

	promptSamples := make([]tokenizer.PromptSample, 0, len(samples))
	for _, sample := range samples {
		promptSamples = append(promptSamples, tokenizer.PromptSample{
			Visible: sample.Visible,
			HeldOut: sample.Answer,
			Full:    sample.Full,
		})
	}

	experiment.prompt = tokenizer.NewPrompt(
		tokenizer.PromptWithSamples(promptSamples),
	)

	return experiment.prompt
}

func (experiment *BabiExperiment) Holdout() (int, tokenizer.HoldoutType) {
	return 0, tokenizer.RIGHT
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
	return experiment.Score(), gc.ShouldBeGreaterThan, 0.0
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

	// ── Compute summary statistics ──────────────────────────────
	exactMatches := 0
	partialSum := 0.0

	// Per-entity tracking
	entityStats := map[string]*entityStat{}
	locationStats := map[string]*locationStat{}
	var failures []failureRecord

	for _, row := range experiment.tableData {
		expected := string(row.Holdout)
		observed := string(row.Observed)

		// Entity from the question: parse "Where is X?" from prefix
		entity := extractEntityFromPrefix(string(row.Prefix))
		if entity == "" {
			entity = "unknown"
		}

		if row.Scores.Exact == 1.0 {
			exactMatches++
		} else {
			failures = append(failures, failureRecord{
				Idx:      row.Idx,
				Entity:   entity,
				Expected: expected,
				Observed: observed,
			})
		}
		partialSum += row.Scores.Partial

		// Entity stats
		es, ok := entityStats[entity]
		if !ok {
			es = &entityStat{}
			entityStats[entity] = es
		}
		es.Total++
		if row.Scores.Exact == 1.0 {
			es.Correct++
		}

		// Location stats
		ls, ok := locationStats[expected]
		if !ok {
			ls = &locationStat{}
			locationStats[expected] = ls
		}
		ls.Total++
		if row.Scores.Exact == 1.0 {
			ls.Correct++
		}
	}

	exactRate := float64(exactMatches) / float64(n)
	partialRate := partialSum / float64(n)

	// ── Build per-entity accuracy series for the combo chart ─────
	entityNames := sortedKeys(entityStats)
	entityAccData := make([]float64, len(entityNames))
	entityCountData := make([]float64, len(entityNames))
	for i, name := range entityNames {
		es := entityStats[name]
		entityAccData[i] = float64(es.Correct) / float64(es.Total)
		entityCountData[i] = float64(es.Total)
	}

	// ── Build per-location accuracy series for the bar chart ─────
	locNames := sortedKeys(locationStats)
	locAccData := make([]float64, len(locNames))
	locCountData := make([]float64, len(locNames))
	for i, name := range locNames {
		ls := locationStats[name]
		locAccData[i] = float64(ls.Correct) / float64(ls.Total)
		locCountData[i] = float64(ls.Total)
	}

	// ── Build failure table rows ────────────────────────────────
	maxFailures := 20
	if len(failures) < maxFailures {
		maxFailures = len(failures)
	}
	failureRows := make([][]string, maxFailures)
	for i := 0; i < maxFailures; i++ {
		f := failures[i]
		failureRows[i] = []string{
			fmt.Sprintf("%d", f.Idx),
			f.Entity,
			f.Expected,
			f.Observed,
		}
	}

	// ── Build the prose template and data ────────────────────────
	proseTemplate := `\subsection{bAbI QA Task 1: Single Supporting Fact}
\label{sec:babi_benchmark}

\paragraph{Task Description.}
The bAbI QA benchmark (Task~1) evaluates single-supporting-fact
question answering. Each sample consists of a short story describing
entity movements between locations, followed by a question of the
form \textit{''Where is Entity?''}. The correct
answer is the last location the entity moved to.

\paragraph{Test Conditions.}
The experiment evaluated {{.NSamples}} samples from the
\texttt{facebook/babi\_qa} dataset (subset \texttt{en-10k-qa1}).
Reasoning was performed using TransitiveResonance-validated
entity tracking: the system extracts the entity name from the
question, identifies its last movement sentence in the story,
and emits the location word. TransitiveResonance computes a
geometric validation score measuring structural plausibility
of the answer chord against the hypothesis residue.

\paragraph{Results.}
The system achieved an exact-match accuracy of {{.ExactRate | pct}}
across all {{.NSamples}} samples, with a mean partial score of
{{.PartialRate | f3}}.
{{- if gt .NFailures 0}}
A total of {{.NFailures}} samples failed exact match.
{{- if le .NFailures 20}}
Table~\ref{tab:babi_failures} lists all failure cases.
{{- else}}
Table~\ref{tab:babi_failures} lists the first 20 failure cases.
{{- end}}
{{- end}}

\paragraph{Per-Entity Analysis.}
{{- range .EntitySummary}}
\textbf{ {{- .Name -}} }: {{.Correct}}/{{.Total}} correct ({{.Rate | pct}}).
{{- end}}

\paragraph{Per-Location Analysis.}
{{- range .LocationSummary}}
\textbf{ {{- .Name -}} }: {{.Correct}}/{{.Total}} correct ({{.Rate | pct}}).
{{- end}}

{{- if gt .NFailures 0}}
\begin{table}[h]
\centering
\caption{bAbI Task~1 failure cases (first {{.NFailureRows}}).
Exact-match accuracy: {{.ExactRate | pct}}.
$N={{.NSamples}}$, subset: \texttt{en-10k-qa1}.}
\label{tab:babi_failures}
\begin{tabular}{|r|l|l|l|}
\hline
\textbf{Idx} & \textbf{Entity} & \textbf{Expected} & \textbf{Observed} \\
\hline
{{- range .FailureRows}}
{{index . 0}} & {{index . 1}} & {{index . 2}} & {{index . 3}} \\
\hline
{{- end}}
\end{tabular}
\end{table}
{{- end}}
`

	// Entity summary structs for template
	type entitySum struct {
		Name    string
		Correct int
		Total   int
		Rate    float64
	}

	entitySummary := make([]entitySum, len(entityNames))
	for i, name := range entityNames {
		es := entityStats[name]
		entitySummary[i] = entitySum{
			Name:    name,
			Correct: es.Correct,
			Total:   es.Total,
			Rate:    float64(es.Correct) / float64(es.Total),
		}
	}

	locationSummary := make([]entitySum, len(locNames))
	for i, name := range locNames {
		ls := locationStats[name]
		locationSummary[i] = entitySum{
			Name:    name,
			Correct: ls.Correct,
			Total:   ls.Total,
			Rate:    float64(ls.Correct) / float64(ls.Total),
		}
	}

	proseData := map[string]any{
		"NSamples":        n,
		"ExactRate":       exactRate,
		"PartialRate":     partialRate,
		"NFailures":       len(failures),
		"NFailureRows":    maxFailures,
		"FailureRows":     failureRows,
		"EntitySummary":   entitySummary,
		"LocationSummary": locationSummary,
	}

	artifacts := []tools.Artifact{
		// 1. Results table: per-sample scores
		{
			Type:     tools.ArtifactTable,
			FileName: "babi_results",
			Data:     experiment.tableData,
			Title:    "bAbI Task 1 — Per-Sample Results",
			Caption: fmt.Sprintf(
				"Per-sample exact/partial/fuzzy scores. N=%d, exact accuracy=%.1f%%, mean partial=%.3f. Subset: en-10k-qa1.",
				n, exactRate*100, partialRate,
			),
			Label: "tab:babi_results",
		},
		// 2. Combo chart: per-entity accuracy + sample counts
		{
			Type:     tools.ArtifactComboChart,
			FileName: "babi_entity_accuracy",
			Data: tools.ComboChartData{
				XAxis: entityNames,
				Series: []tools.ComboSeries{
					{Name: "Accuracy", Type: "bar", Data: entityAccData, BarWidth: "40%"},
					{Name: "Count", Type: "line", Symbol: "circle", Data: entityCountData},
				},
				XName: "Entity",
				YName: "Value",
				YMin:  0,
				YMax:  math.Max(1.0, maxFloat(entityCountData)*1.1),
			},
			Title:   "bAbI Task 1 — Per-Entity Accuracy",
			Caption: fmt.Sprintf("Accuracy (bars) and sample count (line) per entity. N=%d samples.", n),
			Label:   "fig:babi_entity_accuracy",
		},
		// 3. Bar chart: per-location accuracy
		{
			Type:     tools.ArtifactBarChart,
			FileName: "babi_location_accuracy",
			Data: tools.BarChartData{
				XAxis: locNames,
				Series: []tools.BarSeries{
					{Name: "Accuracy", Data: locAccData},
					{Name: "Count (÷10)", Data: divSlice(locCountData, 10)},
				},
			},
			Title:   "bAbI Task 1 — Per-Location Accuracy",
			Caption: fmt.Sprintf("Accuracy per target location. N=%d samples.", n),
			Label:   "fig:babi_location_accuracy",
		},
		// 4. Prose: single .tex section with analysis
		{
			Type:     tools.ArtifactProse,
			FileName: "babi_analysis.tex",
			Data: tools.ProseData{
				Template: proseTemplate,
				Data:     proseData,
			},
			Title: "bAbI Analysis",
			Label: "sec:babi_analysis",
		},
	}

	return artifacts
}

func (experiment *BabiExperiment) Finalize(
	substrate *geometry.HybridSubstrate,
) error {
	return nil
}

// ── Internal helpers ────────────────────────────────────────────────

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
