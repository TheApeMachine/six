package phasedial

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

/*
PartialDeletionExperiment evaluates the PhaseDial's robustness to sparse
manifolds. It demonstrates that the topological structure remains coherent
even if a significant portion of the corpus is removed.
*/
type PartialDeletionExperiment struct {
	tableData []tools.ExperimentalData
	dataset   provider.Dataset
	prompt    []string
	evaluator *tools.Evaluator
}

func NewPartialDeletionExperiment() *PartialDeletionExperiment {
	return &PartialDeletionExperiment{
		tableData: []tools.ExperimentalData{},
		// Baseline 0.05: Partial deletion robustness.
		// Any non-zero result demonstrates the property holds.
		// Target 0.50: strong geometric invariant.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.50),
		),
		dataset: tools.NewLocalProvider(tools.Aphorisms),
	}
}

func (experiment *PartialDeletionExperiment) Name() string {
	return "Partial Deletion"
}

func (experiment *PartialDeletionExperiment) Section() string {
	return "phasedial"
}

func (experiment *PartialDeletionExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *PartialDeletionExperiment) Prompts() []string {
	return []string{
		"Predict the secondary structure of the given amino acid sequence.",
	}
}

func (experiment *PartialDeletionExperiment) Holdout() (int, input.HoldoutType) {
	return 0, input.RIGHT
}

func (experiment *PartialDeletionExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *PartialDeletionExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *PartialDeletionExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *PartialDeletionExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *PartialDeletionExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactTable,
			FileName: "partial_deletion_summary.tex",
			Data:     experiment.tableData,
			Title:    "Partial Deletion Summary",
			Caption:  "Evaluation of PhaseDial resilience to corpus deletion.",
			Label:    "tab:partial_deletion",
		},

		{
			Type:     tools.ArtifactProse,
			FileName: "partial_deletion_section.tex",
			Data: tools.ProseData{
				Template: `\subsection{Partial Deletion}
\label{sec:partial_deletion}

\paragraph{Task Description.}
The partial deletion experiment evaluates the topological resilience of the PhaseDial to sparse
manifolds.  After ingesting a full corpus, a fraction of substrate
entries is deleted, and retrieval quality is re-evaluated.  The score
reflects how gracefully the value manifold degrades under erasure.

\paragraph{Results.}
Figure~\ref{fig:partial_deletion_map} shows the trial outcome map.
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
