package logic

import (
	"fmt"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/local"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/experiment/trialmap"
)

type SemanticAlgebraExperiment struct {
	tableData []tools.ExperimentalData
	dataset   data.Provider
	facts     []string
	prompt    []string
	holdouts  [][]byte
	evaluator *tools.Evaluator
}

var _ tools.WorkspaceTokenObserver = (*SemanticAlgebraExperiment)(nil)

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
		facts:     append([]string(nil), facts...),
		dataset:   local.New(local.WithStrings(facts)),
		// Baseline 0.95: algebraic cancellation in GF(257) is exact.
		// If the stored phase is Roy·is_in·Kitchen and the query cancels
		// Roy·is_in, the residue must be Kitchen exactly. Partial credit
		// is meaningless for this task — it either works or it doesn't.
		// Target 1.0: perfect cancellation is the design goal.
		evaluator: tools.NewEvaluator(
			tools.EvalWithFixedExpectation(0.95, 1.0),
			tools.EvalAssertTarget(),
		),
	}
}

func (experiment *SemanticAlgebraExperiment) Name() string {
	return "holographic_algebra"
}

func (experiment *SemanticAlgebraExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *SemanticAlgebraExperiment) Prompts() []string {
	experiment.prompt = experiment.prompt[:0]
	experiment.holdouts = experiment.holdouts[:0]
	for _, f := range experiment.facts {
		pr, ho := tools.BytePrefixFraction(f, 0.45)
		if ho == "" {
			continue
		}
		experiment.prompt = append(experiment.prompt, pr)
		experiment.holdouts = append(experiment.holdouts, []byte(ho))
	}
	return experiment.prompt
}

func (experiment *SemanticAlgebraExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (*SemanticAlgebraExperiment) ObserveWorkspaceAsTokens() bool { return true }

func (experiment *SemanticAlgebraExperiment) Section() string {
	return "logic"
}

func (experiment *SemanticAlgebraExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *SemanticAlgebraExperiment) Outcome() (any, Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *SemanticAlgebraExperiment) OutcomeForPrompt(idx int) (any, Assertion, any) {
	return experiment.evaluator.OutcomeForPromptConvey(experiment.tableData, idx)
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

	panels := trialmap.TwoScorePanels(experiment.tableData, score, trialmap.StandardTwoPanel(), nil)

	section := tools.ExperimentSection{
		Title: "Semantic Algebra --- GF(257) Fact Cancellation",
		Label: "semantic_algebra",
		TaskDescription: `The semantic algebra experiment evaluates whether the substrate can perform
logical fact cancellation using arithmetic in GF(257).  A set of relational
facts (e.g., \texttt{Roy is\_in Kitchen}) is ingested. At test time the
query presents a partial fact (e.g., \texttt{Roy is\_in}) and the held-out
target is the missing entity (\texttt{Kitchen}).  The value representation
encodes each token as a GF(257) element; fact cancellation reduces to
modular subtraction, and the residue should uniquely identify the answer.`,
		Results: fmt.Sprintf(`Across $N = %d$ test samples the mean weighted score was %s.
Exact cancellation rate: %s.`, n, projector.F3(score), projector.Pct(exactRate)),
		Assessment: semanticAlgebraAssessment(score),
		FigureRef:  "fig:semantic_algebra_map",
	}

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
				Template: projector.ExperimentSectionTmpl,
				Data:     section,
			},
		},
	}
}

func semanticAlgebraAssessment(score float64) string {
	switch {
	case score >= 0.95:
		return `The substrate achieved near-perfect algebraic cancellation, confirming
that the GF(257) arithmetic path is functioning correctly.  This is the
expected result: the operation is exact by construction.`
	case score >= 0.5:
		return `Partial cancellation was observed.  Some facts resolve correctly while
others produce residues that do not uniquely map to the expected entity.
This suggests value collision or boundary detection issues that need
investigation.`
	default:
		return `Cancellation accuracy was low.  The GF(257) arithmetic path is not
producing correct residues at this stage, likely due to incomplete
integration of the phase encoding with the substrate retrieval path.`
	}
}
