package phasedial

import (
	"fmt"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/local"
	"github.com/theapemachine/six/experiment/projector"
)

type AdaptiveSplitExperiment struct {
	tableData    []tools.ExperimentalData
	dataset      data.Provider
	prompt       []string
	holdouts     [][]byte
	evaluator    *tools.Evaluator
	boundaryRows []map[string]any
	summaryRows  []map[string]any
	gapXAxis     []string
	gapGains     []float64
}

func NewAdaptiveSplitExperiment() *AdaptiveSplitExperiment {
	return &AdaptiveSplitExperiment{
		tableData: []tools.ExperimentalData{},
		// Baseline 0.05: Adaptive split geometric property.
		// Any non-zero result demonstrates the property holds.
		// Target 0.50: strong geometric invariant.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.50),
		),
		dataset: local.New(local.WithStrings(tools.Aphorisms)),
	}
}

func (experiment *AdaptiveSplitExperiment) Name() string {
	return "Adaptive Split"
}

func (experiment *AdaptiveSplitExperiment) Section() string {
	return "phasedial"
}

func (experiment *AdaptiveSplitExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *AdaptiveSplitExperiment) Prompts() []string {
	experiment.prompt, experiment.holdouts = aphorismSplitPrompts()
	return experiment.prompt
}

func (experiment *AdaptiveSplitExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *AdaptiveSplitExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *AdaptiveSplitExperiment) Outcome() (any, Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *AdaptiveSplitExperiment) OutcomeForPrompt(idx int) (any, Assertion, any) {
	return tools.EvaluatorOutcomeForPrompt(experiment.evaluator, experiment.tableData, idx)
}

func (experiment *AdaptiveSplitExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, d := range experiment.tableData {
		total += d.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *AdaptiveSplitExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *AdaptiveSplitExperiment) RawOutput() bool { return false }

func (experiment *AdaptiveSplitExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactTable,
			FileName: "adaptive_split_boundaries.tex",
			Data:     experiment.boundaryRows,
			Title:    "Adaptive Split Boundaries",
			Caption:  "Top 5 boundary candidates ranked by combined balance/decoherence.",
			Label:    "tab:adaptive_split_boundaries",
		},
		{
			Type:     tools.ArtifactBarChart,
			FileName: "adaptive_split_gap",
			Data: tools.BarChartData{
				XAxis:  experiment.gapXAxis,
				Series: []tools.BarSeries{{Name: "Best Gain", Data: experiment.gapGains}},
			},
			Title:   "Adaptive Split Gap Experiment",
			Caption: "Best gain for each gap size.",
			Label:   "fig:adaptive_split_gap",
		},
		{
			Type:     tools.ArtifactTable,
			FileName: "adaptive_split_summary.tex",
			Data:     experiment.summaryRows,
			Title:    "Adaptive Split Summary",
			Caption:  "Comparison of adaptive split vs reference split.",
			Label:    "tab:adaptive_split_summary",
		},

		{
			Type:     tools.ArtifactProse,
			FileName: "adaptive_split_section.tex",
			Data: tools.ProseData{
				Template: projector.ExperimentSectionTmpl,
				Data: tools.ExperimentSection{
					Title: "Adaptive Split",
					Label: "adaptive_split",
					TaskDescription: `The adaptive split experiment evaluates the optimal boundary in the PhaseDial for splitting
compositional fingerprints into independently steerable sub-manifolds.
It sweeps candidate boundaries through the residual field and selects
the split that maximises independent perspective shifts while maintaining
structural balance between left and right sub-fingerprints.`,
					Results:    fmt.Sprintf(`The mean weighted score was %s across $N = %d$ samples.`, projector.F3(experiment.Score()), len(experiment.tableData)),
					Assessment: phasedialAssessment(experiment.Score()),
					FigureRef:  "fig:adaptive_split_map",
				},
			},
		},
	}
}
