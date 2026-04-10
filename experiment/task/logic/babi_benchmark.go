package logic

import (
	"fmt"
	"strings"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/huggingface"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/experiment/trialmap"
	"github.com/theapemachine/six/pkg/core/algo"
)

/*
BabiExperiment evaluates question-answering performance using the
facebook/babi_qa dataset (Task 1: single supporting fact).
*/
type BabiExperiment struct {
	tableData []tools.ExperimentalData
	dataset   *huggingface.BabiQADataset
	prompt    []string
	holdouts  [][]byte
	evaluator *tools.Evaluator
}

var (
	_ tools.HoldoutProvider        = (*BabiExperiment)(nil)
	_ tools.WorkspaceTokenObserver = (*BabiExperiment)(nil)
)

var samples = 100

func NewBabiExperiment() *BabiExperiment {
	experiment := &BabiExperiment{
		tableData: []tools.ExperimentalData{},
		dataset: huggingface.NewBabiQA(
			huggingface.DatasetWithRepo("facebook/babi_qa"),
			huggingface.DatasetWithSamples(samples),
			huggingface.DatasetWithSubset("en-10k-qa1"),
			huggingface.DatasetWithTextColumn("story"),
		),
		// Baseline 0.10: bAbI Task 1 answers are named locations (bathroom,
		// garden, kitchen, etc.). Random byte output has near-zero chance of
		// containing a valid location word. Any match is structural evidence.
		// Target 0.70: strong single-supporting-fact reasoning.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExtractionScorer(),
			tools.EvalWithExpectation(0.10, 0.70),
		),
	}

	return experiment
}

func (experiment *BabiExperiment) Name() string {
	return "babi_benchmark"
}

func (experiment *BabiExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *BabiExperiment) Prompts() []string {
	if len(experiment.prompt) > 0 {
		return experiment.prompt
	}

	for p := range experiment.dataset.GeneratePrompts() {
		experiment.prompt = append(experiment.prompt, p.Text)
		if p.HasLabel && strings.TrimSpace(p.Label) != "" {
			experiment.holdouts = append(experiment.holdouts, []byte(strings.TrimSpace(p.Label)))
		} else {
			experiment.holdouts = append(experiment.holdouts, nil)
		}
	}
	return experiment.prompt
}

func (experiment *BabiExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	experiment.Prompts()
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	h := experiment.holdouts[idx]
	if len(h) == 0 {
		return nil, false
	}
	return h, true
}

func (*BabiExperiment) ObserveWorkspaceAsTokens() bool { return true }

func (*BabiExperiment) SeedViralLearn() bool { return true }

func (experiment *BabiExperiment) Section() string {
	return "logic"
}

func (experiment *BabiExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *BabiExperiment) Outcome() (any, Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *BabiExperiment) OutcomeForPrompt(idx int) (any, Assertion, any) {
	return experiment.evaluator.OutcomeForPromptConvey(experiment.tableData, idx)
}

func (experiment *BabiExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *BabiExperiment) TableData() any {
	return experiment.tableData
}

/*
Answer returns the predicted answer label for this QA task.
*/
func (experiment *BabiExperiment) Answer(prediction *algo.Prediction) string {
	return prediction.Label()
}

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
				Observed: string(row.Generation),
			})
		}
	}

	panels := trialmap.TwoScorePanels(experiment.tableData, score, trialmap.BabiTwoPanel(), nil)

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

	// ── Build failure table string ────────────────────────────────
	var failureTable string
	if len(failures) > 0 {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf(`\begin{table}[htbp]
  \centering
  \caption{bAbI Task~1 failure cases (showing first %d of
    %d). $N = %d$, exact accuracy
    %s.}
  \label{tab:babi_failures}
  \begin{tabular}{rlll}
    \toprule
    \textbf{Q\#} & \textbf{Entity} & \textbf{Expected} & \textbf{Observed} \\
    \midrule`,
			maxFail, len(failures), n, projector.Pct(exactRate)))
		for i := 0; i < maxFail; i++ {
			f := failures[i]
			sb.WriteString(fmt.Sprintf("\n    %d & %s & \\texttt{%s} & \\texttt{%s} \\\\",
				f.Idx, f.Entity, f.Expected, f.Observed))
		}
		sb.WriteString(`
    \bottomrule
  \end{tabular}
\end{table}`)
		failureTable = sb.String()
	}

	section := tools.ExperimentSection{
		Title: "bAbI QA Task 1: Single Supporting Fact",
		Label: "babi_benchmark",
		TaskDescription: fmt.Sprintf(`The bAbI QA benchmark (Task~1) evaluates single-supporting-fact
question answering. Each sample consists of a short story describing
entity movements between named locations, followed by a question of
the form \textit{''Where is Person?''}. The correct answer is the
last location the entity moved to---requiring the system to track
an entity through a chain of movement facts without any explicit
pointer to the relevant sentence.

\paragraph{Test Conditions.}
Experiments used %d samples from
\texttt{facebook/babi\_qa} (subset \texttt{en-10k-qa1}).
Reasoning is performed via Transitive Resonance: the entity value is
extracted from the question, the story is scanned geometrically for
its last movement relationship, and the residue value is decoded as
the location answer.`, n),
		Results: fmt.Sprintf(`Figure~\ref{fig:babi_trial_map} shows the per-sample Trial Outcome
Map. Each row of the left heatmap corresponds to one question;
columns show the Exact, Partial, Fuzzy, and Weighted scores on a
0--1 colour scale (viridis, dark = 0, bright = 1). The right
panel displays the weighted score per sample alongside the
overall mean (orange dashed line).

The system achieved an exact-match accuracy of %s
across all %d samples, with a mean partial score of
%s and an overall weighted score of
%s.`, projector.Pct(exactRate), n, projector.F3(partialRate), projector.F3(score)),
		Assessment: babiAssessment(exactRate),
		Table:      failureTable,
		FigureRef:  "fig:babi_trial_map",
	}

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
				Template: projector.ExperimentSectionTmpl,
				Data:     section,
			},
		},
	}
}

func babiAssessment(exactRate float64) string {
	switch {
	case exactRate > 0.7:
		return `The substrate resolved the majority of single-supporting-fact queries
exactly, demonstrating reliable transitive chain traversal through
geometric residue accumulation.`
	case exactRate > 0.3:
		return `The substrate correctly resolved a minority of queries by exact match.
Partial scores indicate that many outputs were geometrically adjacent
to the correct location value, suggesting the attractor is in the
right region but final decoding introduces ambiguity.`
	default:
		return `Exact-match accuracy was low.  The Transitive Resonance mechanism
requires the entity's movement facts to produce a sufficiently
distinct residue value; at this sample size the substrate geometry
may not separate location attractors reliably.`
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
