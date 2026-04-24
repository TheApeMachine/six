package phasedial

import (
	"fmt"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/local"
	"github.com/theapemachine/six/experiment/projector"
)

/*
PhaseCoherenceExperiment performs pairwise phase correlation analysis across
all fingerprints in the corpus. It verifies the periodic and structural
properties of the PhaseDial encoding, such as short-range repulsion and
long-range attraction.
*/
type PhaseCoherenceExperiment struct {
	tableData []tools.ExperimentalData
	dataset   data.Provider
	prompt    []string
	holdouts  [][]byte
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
		dataset: local.New(local.WithStrings(tools.Aphorisms)),
	}
}

func (experiment *PhaseCoherenceExperiment) Name() string {
	return "Phase Coherence"
}

func (experiment *PhaseCoherenceExperiment) Section() string {
	return "phasedial"
}

func (experiment *PhaseCoherenceExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *PhaseCoherenceExperiment) Prompts() []string {
	experiment.prompt, experiment.holdouts = aphorismSplitPrompts()
	return experiment.prompt
}

func (experiment *PhaseCoherenceExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *PhaseCoherenceExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *PhaseCoherenceExperiment) Outcome() (any, Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *PhaseCoherenceExperiment) OutcomeForPrompt(idx int) (any, Assertion, any) {
	return experiment.evaluator.OutcomeForPromptConvey(experiment.tableData, idx)
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
	n := len(experiment.tableData)
	score := experiment.Score()
	return PhasedialSectionArtifacts(
		"Phase Coherence",
		experiment.tableData,
		score,
		tools.ExperimentSection{
			Title: "Phase Coherence",
			Label: "phase_coherence",
			TaskDescription: `The phase coherence experiment evaluates whether the PhaseDial maintains
internal consistency after multiple rounds of composition and retrieval.
Starting from a seed entry, the system performs sequential hops; at
each step the coherence between the current fingerprint and the
original is measured.

High coherence indicates that the manifold's geometric structure is
stable under composition --- each hop navigates to a structurally
related region rather than diverging into noise.`,
			Results:    fmt.Sprintf(`Across $N = %d$ test samples the mean weighted score was %s.`, n, projector.F3(score)),
			Assessment: phasedialAssessment(score),
			FigureRef:  "fig:phase_coherence_map",
		},
	)
}
