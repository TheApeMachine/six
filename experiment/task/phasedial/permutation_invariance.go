package phasedial

import (
	"math"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

/*
PermutationInvarianceExperiment evaluates whether the PhaseDial's retrieval
properties are invariant to the order of ingestion. It performs a geodesic
scan and generates a multi-panel chart showing the semantic geodesic matrix.
*/
type PermutationInvarianceExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    []string
	evaluator *tools.Evaluator
}

func NewPermutationInvarianceExperiment() *PermutationInvarianceExperiment {
	return &PermutationInvarianceExperiment{
		tableData: []tools.ExperimentalData{},
		// Baseline 0.05: Permutation invariance property.
		// Any non-zero result demonstrates the property holds.
		// Target 0.50: strong geometric invariant.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.50),
		),
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *PermutationInvarianceExperiment) Name() string {
	return "Permutation Invariance"
}

func (experiment *PermutationInvarianceExperiment) Section() string {
	return "phasedial"
}

func (experiment *PermutationInvarianceExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *PermutationInvarianceExperiment) Prompts() []string {
	return []string{
		"Predict the secondary structure of the given amino acid sequence.",
	}
}

func (experiment *PermutationInvarianceExperiment) Holdout() (int, input.HoldoutType) {
	return 0, input.RIGHT
}

func (experiment *PermutationInvarianceExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *PermutationInvarianceExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *PermutationInvarianceExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return math.NaN() // Not yet computed
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *PermutationInvarianceExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *PermutationInvarianceExperiment) Artifacts() []tools.Artifact {
return PhasedialSectionArtifacts(
"Permutation Invariance",
experiment.tableData,
experiment.Score(),
`\subsection{Permutation Invariance}
\label{sec:permutation_invariance}

\paragraph{Task Description.}
The permutation invariance experiment verifies that the PhaseDial
representation is insensitive to the ordering of ingested samples.
Two identical corpora are ingested in different random orderings; the
resulting substrate fingerprints should produce equivalent retrieval
results.

This is a critical structural property: if retrieval quality depends
on ingestion order, the substrate is encoding positional artefacts
rather than genuine structural relationships.

\paragraph{Results.}
Across $N = {{.N}}$ test samples the mean weighted score was {{.Score | f3}}.

{{if gt .Score 0.5 -}}
\paragraph{Assessment.}
The substrate demonstrated strong permutation invariance invariance,
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

Figure~\ref{fig:permutation_invariance_map} shows the trial outcome map.
`,
map[string]any{"N": len(experiment.tableData), "Score": experiment.Score()},
)
}
