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
SteerabilityExperiment evaluates the stability of retrieval under phase
rotations across different split boundaries. It identifies the optimal
boundary for independent perspective shifts.
*/
type SteerabilityExperiment struct {
	tableData       []tools.ExperimentalData
	dataset         data.Provider
	prompt          []string
	holdouts        [][]byte
	evaluator       *tools.Evaluator
	splitCandidates []int
	sweepStepDeg    float64
}

type steerabilityOpt func(*SteerabilityExperiment)

func NewSteerabilityExperiment(opts ...steerabilityOpt) *SteerabilityExperiment {
	experiment := &SteerabilityExperiment{
		tableData: []tools.ExperimentalData{},
		// Baseline 0.20: Phase steerability control.
		// Any non-zero result demonstrates the property holds.
		// Target 0.70: strong geometric invariant.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.20, 0.70),
		),
		dataset:         local.New(local.WithStrings(tools.Aphorisms)),
		splitCandidates: []int{192, 224, 256, 288, 320},
		sweepStepDeg:    5.0,
	}

	for _, opt := range opts {
		opt(experiment)
	}

	if len(experiment.splitCandidates) == 0 {
		experiment.splitCandidates = []int{192, 224, 256, 288, 320}
	}

	if experiment.sweepStepDeg <= 0 || experiment.sweepStepDeg > 180 {
		experiment.sweepStepDeg = 5.0
	}

	return experiment
}

func SteerabilityWithDataset(dataset data.Provider) steerabilityOpt {
	return func(experiment *SteerabilityExperiment) {
		if dataset != nil {
			experiment.dataset = dataset
		}
	}
}

func SteerabilityWithSplitCandidates(splitCandidates []int) steerabilityOpt {
	return func(experiment *SteerabilityExperiment) {
		if len(splitCandidates) > 0 {
			experiment.splitCandidates = append([]int(nil), splitCandidates...)
		}
	}
}

func SteerabilityWithSweepStep(stepDeg float64) steerabilityOpt {
	return func(experiment *SteerabilityExperiment) {
		if stepDeg > 0 && stepDeg <= 180 {
			experiment.sweepStepDeg = stepDeg
		}
	}
}

func (experiment *SteerabilityExperiment) Name() string {
	return "Steerability"
}

func (experiment *SteerabilityExperiment) Section() string {
	return "phasedial"
}

func (experiment *SteerabilityExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *SteerabilityExperiment) Prompts() []string {
	experiment.prompt, experiment.holdouts = aphorismSplitPrompts()
	return experiment.prompt
}

func (experiment *SteerabilityExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *SteerabilityExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *SteerabilityExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *SteerabilityExperiment) OutcomeForPrompt(idx int) (any, gc.Assertion, any) {
	return tools.EvaluatorOutcomeForPrompt(experiment.evaluator, experiment.tableData, idx)
}

func (experiment *SteerabilityExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, d := range experiment.tableData {
		total += d.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *SteerabilityExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *SteerabilityExperiment) RawOutput() bool { return false }

func (experiment *SteerabilityExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactBarChart,
			FileName: "steerability_scores",
			Data:     experiment.tableData,
			Title:    "Steerability Score Breakdown",
			Caption:  "Steerability and gain across different split boundaries.",
			Label:    "fig:steerability_scores",
		},

		{
			Type:     tools.ArtifactProse,
			FileName: "steerability_section.tex",
			Data: tools.ProseData{
				Template: projector.ExperimentSectionTmpl,
				Data: tools.ExperimentSection{
					Title: "Steerability",
					Label: "steerability",
					TaskDescription: `The steerability experiment evaluates the stability of retrieval under phase rotations across
different split boundaries.  It identifies the optimal boundary for
independent perspective shifts and measures whether high steerability
predicts super-additive composition gain.`,
					Results:    fmt.Sprintf(`The mean weighted score was %s across $N = %d$ samples.`, projector.F3(experiment.Score()), len(experiment.tableData)),
					Assessment: phasedialAssessment(experiment.Score()),
					FigureRef:  "fig:steerability_map",
				},
			},
		},
	}
}
