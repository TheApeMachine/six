package scaling

import (
	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
)

/*
SubstrateQueryScalingExperiment loads a 400-sample synthetic corpus and issues
50 prefix queries spread across corpus depth. The scaling question: does query
accuracy degrade as the corpus grows — do queries targeting sample 5 (early
ingest) behave differently from sample 350 (late ingest)?
*/
type SubstrateQueryScalingExperiment struct {
	tableData []tools.ExperimentalData
	dataset   data.Provider
	prompt    []string
	holdouts  [][]byte
	evaluator *tools.Evaluator
}

func NewSubstrateQueryScalingExperiment() *SubstrateQueryScalingExperiment {
	return &SubstrateQueryScalingExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   NewSyntheticDataset(128, 400, 42),
		evaluator: tools.NewEvaluator(
			tools.EvalWithScalingInstrumentScorer(),
			tools.EvalWithExpectation(0.05, 0.50),
		),
	}
}

func (experiment *SubstrateQueryScalingExperiment) Name() string {
	return "Substrate query scaling"
}

func (experiment *SubstrateQueryScalingExperiment) Section() string { return "scaling" }

func (experiment *SubstrateQueryScalingExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *SubstrateQueryScalingExperiment) Prompts() []string {
	ds, ok := experiment.dataset.(*SyntheticDataset)
	if !ok {
		return nil
	}

	experiment.prompt, experiment.holdouts = syntheticSamplePrompts(ds, 50, 32)
	return experiment.prompt
}

func (experiment *SubstrateQueryScalingExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}

	return experiment.holdouts[idx], true
}

func (experiment *SubstrateQueryScalingExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *SubstrateQueryScalingExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *SubstrateQueryScalingExperiment) OutcomeForPrompt(idx int) (any, gc.Assertion, any) {
	return tools.EvaluatorOutcomeForPrompt(experiment.evaluator, experiment.tableData, idx)
}

func (experiment *SubstrateQueryScalingExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *SubstrateQueryScalingExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *SubstrateQueryScalingExperiment) Artifacts() []tools.Artifact {
	return SubstrateQueryScalingArtifacts(experiment.tableData)
}

func (experiment *SubstrateQueryScalingExperiment) RawOutput() bool { return false }

var _ tools.HoldoutProvider = (*SubstrateQueryScalingExperiment)(nil)
