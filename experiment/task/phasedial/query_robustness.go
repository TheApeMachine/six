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
QueryRobustnessExperiment evaluates the topological resilience of the PhaseDial
to corrupted inputs. It demonstrates that the system can resolve correct
readouts from queries with 30% character dropout by scanning the phase torus.
*/
type QueryRobustnessExperiment struct {
	tableData         []tools.ExperimentalData
	robustnessResults []robustnessEntry
	dataset           data.Provider
	prompt            []string
	holdouts          [][]byte
	evaluator         *tools.Evaluator
}

func NewQueryRobustnessExperiment() *QueryRobustnessExperiment {
	return &QueryRobustnessExperiment{
		tableData: []tools.ExperimentalData{},
		// Baseline 0.20: Query robustness under perturbation.
		// Any non-zero result demonstrates the property holds.
		// Target 0.60: strong geometric invariant.
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.20, 0.60),
		),
		robustnessResults: []robustnessEntry{},
		dataset:           local.New(local.WithStrings(tools.Aphorisms)),
	}
}

func (experiment *QueryRobustnessExperiment) Name() string {
	return "Query Robustness"
}

func (experiment *QueryRobustnessExperiment) Section() string {
	return "phasedial"
}

func (experiment *QueryRobustnessExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *QueryRobustnessExperiment) Prompts() []string {
	experiment.prompt, experiment.holdouts = aphorismSplitPrompts()
	return experiment.prompt
}

func (experiment *QueryRobustnessExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}
	return experiment.holdouts[idx], true
}

func (experiment *QueryRobustnessExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *QueryRobustnessExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *QueryRobustnessExperiment) OutcomeForPrompt(idx int) (any, gc.Assertion, any) {
	return experiment.evaluator.OutcomeForPromptConvey(experiment.tableData, idx)
}

func (experiment *QueryRobustnessExperiment) Score() float64 {
	if len(experiment.tableData) == 0 {
		return 0
	}
	total := 0.0
	for _, data := range experiment.tableData {
		total += data.WeightedTotal
	}
	return total / float64(len(experiment.tableData))
}

func (experiment *QueryRobustnessExperiment) TableData() any {
	return experiment.tableData
}

type robustnessEntry struct {
	Query      string
	ScanSteps  int
	Step0Match string
	CorruptSim string
}

func (experiment *QueryRobustnessExperiment) Artifacts() []tools.Artifact {
	return []tools.Artifact{
		{
			Type:     tools.ArtifactTable,
			FileName: "query_robustness_summary.tex",
			Data:     experiment.robustnessResults,
			Title:    "Query Robustness Summary",
			Caption:  "Resilience of PhaseDial retrieval to character dropout.",
			Label:    "tab:query_robustness",
		},

		{
			Type:     tools.ArtifactProse,
			FileName: "query_robustness_section.tex",
			Data: tools.ProseData{
				Template: projector.ExperimentSectionTmpl,
				Data: tools.ExperimentSection{
					Title: "Query Robustness",
					Label: "query_robustness",
					TaskDescription: `The query robustness experiment evaluates the topological resilience of the PhaseDial to corrupted
inputs.  A clean query is compared against a version with 30\% value
dropout; both are submitted to geodesic scan.  The score reflects
how accurately the substrate recovers the same retrieval target
despite input corruption.`,
					Results:    fmt.Sprintf(`The mean weighted score was %s across $N = %d$ samples.`, projector.F3(experiment.Score()), len(experiment.tableData)),
					Assessment: phasedialAssessment(experiment.Score()),
					FigureRef:  "fig:query_robustness_map",
				},
			},
		},
	}
}

func (experiment *QueryRobustnessExperiment) RawOutput() bool { return false }

