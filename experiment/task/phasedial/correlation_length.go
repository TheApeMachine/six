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
CorrelationLengthExperiment evaluates how the PhaseDial exploits the
correlation length of sequences. It tests various block partitions (hard vs
overlapping) to identify where super-additive gain is achieved, proving that
hard boundaries are necessary for structural independence.
*/
type CorrelationLengthExperiment struct {
	tableData []tools.ExperimentalData
	dataset   data.Provider
	prompt    []string
	holdouts  [][]byte
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
		dataset: local.New(local.WithStrings(tools.Aphorisms)),
	}
}

func (experiment *CorrelationLengthExperiment) Name() string {
	return "Correlation Length"
}

func (experiment *CorrelationLengthExperiment) Section() string {
	return "phasedial"
}

func (experiment *CorrelationLengthExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *CorrelationLengthExperiment) Prompts() []string {
	experiment.prompt, experiment.holdouts = aphorismSplitPrompts()
	return experiment.prompt
}

func (experiment *CorrelationLengthExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *CorrelationLengthExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *CorrelationLengthExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *CorrelationLengthExperiment) OutcomeForPrompt(idx int) (any, gc.Assertion, any) {
	return tools.EvaluatorOutcomeForPrompt(experiment.evaluator, experiment.tableData, idx)
}

func (experiment *CorrelationLengthExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0.0 // No data yet
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
	n := len(experiment.tableData)
	score := experiment.Score()
	return PhasedialSectionArtifacts(
		"Correlation Length",
		experiment.tableData,
		score,
		tools.ExperimentSection{
			Title: "Correlation Length",
			Label: "correlation_length",
			TaskDescription: `The correlation length experiment measures the spatial decay of value
similarity as a function of angular distance on the phase torus.
Starting from a seed fingerprint, the system rotates in fixed angular
increments and measures how quickly similarity to the original decays.
The decay rate characterises the \textit{correlation length} of the value
manifold --- the angular radius within which attractor influence is
detectable.

A well-structured manifold should exhibit a clean exponential or
power-law decay, indicating that nearby regions share structural
information while distant regions are independent.`,
			Results:    fmt.Sprintf(`Across $N = %d$ test samples the mean weighted score was %s.`, n, projector.F3(score)),
			Assessment: phasedialAssessment(score),
			FigureRef:  "fig:correlation_length_map",
		},
	)
}
