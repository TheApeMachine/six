package phasedial

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

/*
SteerabilityExperiment evaluates the stability of retrieval under phase
rotations across different split boundaries. It identifies the optimal
boundary for independent perspective shifts.
*/
type SteerabilityExperiment struct {
	tableData       []tools.ExperimentalData
	dataset         provider.Dataset
	prompt          []string
	evaluator *tools.Evaluator
	accuracy        float64
	splitCandidates []int
	sweepStepDeg    float64
}

type steerabilityOpt func(*SteerabilityExperiment)

func NewSteerabilityExperiment(opts ...steerabilityOpt) *SteerabilityExperiment {
	experiment := &SteerabilityExperiment{
		tableData:       []tools.ExperimentalData{},
		// Baseline 0.20: Phase steerability control.
		// Any non-zero result demonstrates the property holds.
		// Target 0.70: strong geometric invariant.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.20, 0.70),
		),
		dataset:         tools.NewLocalProvider(tools.Aphorisms),
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

func SteerabilityWithDataset(dataset provider.Dataset) steerabilityOpt {
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

func (experiment *SteerabilityExperiment) Dataset() provider.Dataset {
	return experiment.dataset
}

func (experiment *SteerabilityExperiment) Prompts() []string {
	return []string{
		"Predict the secondary structure of the given amino acid sequence.",
	}
}

func (experiment *SteerabilityExperiment) Holdout() (int, input.HoldoutType) {
	return 0, input.RIGHT
}

func (experiment *SteerabilityExperiment) AddResult(results tools.ExperimentalData) {
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *SteerabilityExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *SteerabilityExperiment) Score() float64 {
	return experiment.accuracy
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
Template: `\subsection{Steerability}
\label{sec:steerability}

\paragraph{Task Description.}
The steerability experiment evaluates the stability of retrieval under phase rotations across
different split boundaries.  It identifies the optimal boundary for
independent perspective shifts and measures whether high steerability
predicts super-additive composition gain.

\paragraph{Results.}
Figure~\ref{fig:steerability_map} shows the trial outcome map.
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
