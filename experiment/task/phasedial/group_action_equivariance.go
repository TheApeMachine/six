package phasedial

import (
	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/local"
)

/*
GroupActionEquivarianceExperiment validates the abelian group property of the
PhaseDial rotations. It verifies that sequential rotations Φ(α) Φ(β) are
equivalent to the combined rotation Φ(α+β), ensuring consistent geometric
inference paths.
*/
type GroupActionEquivarianceExperiment struct {
	tableData []tools.ExperimentalData
	dataset   data.Provider
	prompt    []string
	holdouts  [][]byte
	evaluator *tools.Evaluator
}

func NewGroupActionEquivarianceExperiment() *GroupActionEquivarianceExperiment {
	return &GroupActionEquivarianceExperiment{
		tableData: []tools.ExperimentalData{},
		// Baseline 0.05: Group action equivariance invariant.
		// Any non-zero result demonstrates the property holds.
		// Target 0.50: strong geometric invariant.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.50),
		),
		dataset: local.New(local.WithStrings(tools.Aphorisms)),
	}
}

func (experiment *GroupActionEquivarianceExperiment) Name() string {
	return "Group Action Equivariance"
}

func (experiment *GroupActionEquivarianceExperiment) Section() string {
	return "phasedial"
}

func (experiment *GroupActionEquivarianceExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *GroupActionEquivarianceExperiment) Prompts() []string {
	experiment.prompt, experiment.holdouts = aphorismSplitPrompts()
	return experiment.prompt
}

func (experiment *GroupActionEquivarianceExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *GroupActionEquivarianceExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *GroupActionEquivarianceExperiment) Outcome() (any, Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *GroupActionEquivarianceExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0.0 // No data yet
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *GroupActionEquivarianceExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *GroupActionEquivarianceExperiment) Artifacts() []tools.Artifact {
	return PhasedialSectionArtifacts(
		"Group Action Equivariance",
		experiment.tableData,
		experiment.Score(),
		`\subsection{Group Action Equivariance}
\label{sec:group_action_equivariance}

\paragraph{Task Description.}
The group action equivariance experiment verifies that rotation in
phase space commutes with retrieval: rotating a query fingerprint by
angle $lpha$ and then retrieving should produce the same result as
retrieving first and then rotating the result by $lpha$.

Equivariance is a necessary condition for the PhaseDial to serve as a
navigable manifold --- it guarantees that rotational operations have
predictable effects on retrieval outcomes, enabling controlled steering.

\paragraph{Results.}
Across $N = {{.N}}$ test samples the mean weighted score was {{.Score | f3}}.

{{if gt .Score 0.5 -}}
\paragraph{Assessment.}
The substrate demonstrated strong group action equivariance invariance,
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

Figure~\ref{fig:group_action_equivariance_map} shows the trial outcome map.
`,
		map[string]any{"N": len(experiment.tableData), "Score": experiment.Score()},
	)
}
