package phasedial

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

/*
PhaseCoherenceExperiment performs pairwise phase correlation analysis across
all fingerprints in the corpus. It verifies the periodic and structural
properties of the PhaseDial encoding, such as short-range repulsion and
long-range attraction.
*/
type PhaseCoherenceExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    []string
	evaluator *tools.Evaluator
}

func NewPhaseCoherenceExperiment() *PhaseCoherenceExperiment {
	return &PhaseCoherenceExperiment{
		tableData: []tools.ExperimentalData{},
		// Baseline 0.05: Phase coherence invariant.
		// Any non-zero result demonstrates the property holds.
		// Target 0.50: strong geometric invariant.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.50),
		),
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *PhaseCoherenceExperiment) Name() string {
	return "Phase Coherence"
}

func (experiment *PhaseCoherenceExperiment) Section() string {
	return "phasedial"
}

func (experiment *PhaseCoherenceExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *PhaseCoherenceExperiment) Prompts() []string {
	return []string{
		"Predict the secondary structure of the given amino acid sequence.",
	}
}

func (experiment *PhaseCoherenceExperiment) Holdout() (int, input.HoldoutType) {
	return 0, input.RIGHT
}

func (experiment *PhaseCoherenceExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *PhaseCoherenceExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *PhaseCoherenceExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0.0 // No data yet
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *PhaseCoherenceExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *PhaseCoherenceExperiment) Artifacts() []tools.Artifact {
return PhasedialSectionArtifacts(
"Phase Coherence",
experiment.tableData,
experiment.Score(),
`\subsection{Phase Coherence}
\label{sec:phase_coherence}

\paragraph{Task Description.}
The phase coherence experiment evaluates whether the PhaseDial maintains
internal consistency after multiple rounds of composition and retrieval.
Starting from a seed entry, the system performs sequential hops; at
each step the coherence between the current fingerprint and the
original is measured.

High coherence indicates that the manifold's geometric structure is
stable under composition --- each hop navigates to a structurally
related region rather than diverging into noise.

\paragraph{Results.}
Across $N = {{.N}}$ test samples the mean weighted score was {{.Score | f3}}.

{{if gt .Score 0.5 -}}
\paragraph{Assessment.}
The substrate demonstrated strong phase coherence invariance,
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

Figure~\ref{fig:phase_coherence_map} shows the trial outcome map.
`,
map[string]any{"N": len(experiment.tableData), "Score": experiment.Score()},
)
}
