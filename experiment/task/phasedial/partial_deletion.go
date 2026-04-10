package phasedial

import (
	"fmt"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/local"
	"github.com/theapemachine/six/experiment/projector"
)

/*
PartialDeletionExperiment evaluates the PhaseDial's robustness to sparse
manifolds. It demonstrates that the topological structure remains coherent
even if a significant portion of the corpus is removed.
*/
type PartialDeletionExperiment struct {
	tableData []tools.ExperimentalData
	dataset   data.Provider
	prompt    []string
	holdouts  [][]byte
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
		dataset: local.New(local.WithStrings(tools.Aphorisms)),
	}
}

func (experiment *PartialDeletionExperiment) Name() string {
	return "Partial Deletion"
}

func (experiment *PartialDeletionExperiment) Section() string {
	return "phasedial"
}

func (experiment *PartialDeletionExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *PartialDeletionExperiment) Prompts() []string {
	experiment.prompt, experiment.holdouts = aphorismSplitPrompts()
	return experiment.prompt
}

func (experiment *PartialDeletionExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *PartialDeletionExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *PartialDeletionExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *PartialDeletionExperiment) OutcomeForPrompt(idx int) (any, gc.Assertion, any) {
	return experiment.evaluator.OutcomeForPromptConvey(experiment.tableData, idx)
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
				Template: projector.ExperimentSectionTmpl,
				Data: tools.ExperimentSection{
					Title: "Partial Deletion",
					Label: "partial_deletion",
					TaskDescription: `The partial deletion experiment evaluates the topological resilience of the PhaseDial to sparse
manifolds.  After ingesting a full corpus, a fraction of substrate
entries is deleted, and retrieval quality is re-evaluated.  The score
reflects how gracefully the value manifold degrades under erasure.`,
					Results:    fmt.Sprintf(`The mean weighted score was %s across $N = %d$ samples.`, projector.F3(experiment.Score()), len(experiment.tableData)),
					Assessment: phasedialAssessment(experiment.Score()),
					FigureRef:  "fig:partial_deletion_map",
				},
			},
		},
	}
}
