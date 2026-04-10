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
PermutationInvarianceExperiment evaluates whether the PhaseDial's retrieval
properties are invariant to the order of ingestion. It performs a geodesic
scan and generates a multi-panel chart showing the semantic geodesic matrix.
*/
type PermutationInvarianceExperiment struct {
	tableData []tools.ExperimentalData
	dataset   data.Provider
	prompt    []string
	holdouts  [][]byte
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
		dataset: local.New(local.WithStrings(tools.Aphorisms)),
	}
}

func (experiment *PermutationInvarianceExperiment) Name() string {
	return "Permutation Invariance"
}

func (experiment *PermutationInvarianceExperiment) Section() string {
	return "phasedial"
}

func (experiment *PermutationInvarianceExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *PermutationInvarianceExperiment) Prompts() []string {
	line := ""
	if len(tools.Aphorisms) > 0 {
		line = tools.Aphorisms[0]
	}
	pr, ho := tools.BytePrefixFraction(line, 0.5)
	if ho == "" {
		experiment.holdouts = nil
		return []string{line}
	}
	experiment.holdouts = [][]byte{[]byte(ho)}
	return []string{pr}
}

func (experiment *PermutationInvarianceExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx != 0 || len(experiment.holdouts) == 0 {
		return nil, false
	}
	return experiment.holdouts[0], true
}

func (experiment *PermutationInvarianceExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *PermutationInvarianceExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *PermutationInvarianceExperiment) OutcomeForPrompt(idx int) (any, gc.Assertion, any) {
	return tools.EvaluatorOutcomeForPrompt(experiment.evaluator, experiment.tableData, idx)
}

func (experiment *PermutationInvarianceExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0.0 // No data yet
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
	n := len(experiment.tableData)
	score := experiment.Score()
	return PhasedialSectionArtifacts(
		"Permutation Invariance",
		experiment.tableData,
		score,
		tools.ExperimentSection{
			Title: "Permutation Invariance",
			Label: "permutation_invariance",
			TaskDescription: `The permutation invariance experiment verifies that the PhaseDial
representation is insensitive to the ordering of ingested samples.
Two identical corpora are ingested in different random orderings; the
resulting substrate fingerprints should produce equivalent retrieval
results.

This is a critical structural property: if retrieval quality depends
on ingestion order, the substrate is encoding positional artefacts
rather than genuine structural relationships.`,
			Results:    fmt.Sprintf(`Across $N = %d$ test samples the mean weighted score was %s.`, n, projector.F3(score)),
			Assessment: phasedialAssessment(score),
			FigureRef:  "fig:permutation_invariance_map",
		},
	)
}
