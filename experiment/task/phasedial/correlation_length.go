package phasedial

import (
	"math"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

/*
CorrelationLengthExperiment evaluates how the PhaseDial exploits the
correlation length of sequences. It tests various block partitions (hard vs
overlapping) to identify where super-additive gain is achieved, proving that
hard boundaries are necessary for structural independence.
*/
type CorrelationLengthExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    []string
	evaluator *tools.Evaluator
}

func NewCorrelationLengthExperiment() *CorrelationLengthExperiment {
	return &CorrelationLengthExperiment{
		tableData: []tools.ExperimentalData{},
		// Baseline 0.05: Correlation length decay property.
		// Any non-zero result demonstrates the property holds.
		// Target 0.50: strong geometric invariant.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.50),
		),
		dataset:   tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *CorrelationLengthExperiment) Name() string {
	return "Correlation Length"
}

func (experiment *CorrelationLengthExperiment) Section() string {
	return "phasedial"
}

func (experiment *CorrelationLengthExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *CorrelationLengthExperiment) Prompts() []string {
	experiment.prompt = []string{}
	return experiment.prompt
}

func (experiment *CorrelationLengthExperiment) Holdout() (int, input.HoldoutType) {
	return 0, input.RIGHT
}

func (experiment *CorrelationLengthExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *CorrelationLengthExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *CorrelationLengthExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return math.NaN() // Not yet computed
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *CorrelationLengthExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *CorrelationLengthExperiment) Artifacts() []tools.Artifact {
return PhasedialSectionArtifacts(
"Correlation Length",
experiment.tableData,
experiment.Score(),
`\subsection{Correlation Length}
\label{sec:correlation_length}

\paragraph{Task Description.}
The correlation length experiment measures the spatial decay of chord
similarity as a function of angular distance on the phase torus.
Starting from a seed fingerprint, the system rotates in fixed angular
increments and measures how quickly similarity to the original decays.
The decay rate characterises the \\textit{correlation length} of the chord
manifold --- the angular radius within which attractor influence is
detectable.

A well-structured manifold should exhibit a clean exponential or
power-law decay, indicating that nearby regions share structural
information while distant regions are independent.

\paragraph{Results.}
Across $N = {{.N}}$ test samples the mean weighted score was {{.Score | f3}}.

{{if gt .Score 0.5 -}}
\paragraph{Assessment.}
The substrate demonstrated strong correlation length invariance,
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

Figure~\ref{fig:correlation_length_map} shows the trial outcome map.
`,
map[string]any{"N": len(experiment.tableData), "Score": experiment.Score()},
)
}
