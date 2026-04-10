package scaling

import (
	"fmt"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
)

/*
CompressionExperiment measures how de-duplication efficiency scales with
corpus depth. Ingests 200 samples (128 B each) and issues 50 prefix queries
drawn from positions spread across the corpus. By examining retrieval
quality as a function of sample index (corpus depth), we see whether
compression degrades fidelity for data ingested earlier versus later.
*/
type CompressionExperiment struct {
	tableData []tools.ExperimentalData
	dataset   data.Provider
	prompt    []string
	holdouts  [][]byte
	evaluator *tools.Evaluator
	nSamples  int
	sampleLen int
}

func NewCompressionExperiment() *CompressionExperiment {
	return &CompressionExperiment{
		tableData: []tools.ExperimentalData{},
		dataset:   NewSyntheticDataset(128, 200, 99),
		nSamples:  200,
		sampleLen: 128,
		evaluator: tools.NewEvaluator(
			tools.EvalWithScalingInstrumentScorer(),
			tools.EvalWithExpectation(0.05, 0.80),
		),
	}
}

func (experiment *CompressionExperiment) Name() string    { return "Compression" }
func (experiment *CompressionExperiment) Section() string { return "scaling" }
func (experiment *CompressionExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *CompressionExperiment) Prompts() []string {
	ds, ok := experiment.dataset.(*SyntheticDataset)
	if !ok {
		experiment.prompt = experiment.prompt[:0]
		return experiment.prompt
	}

	experiment.prompt, experiment.holdouts = syntheticSamplePrompts(ds, 50, 32)
	return experiment.prompt
}

func (experiment *CompressionExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}

	return experiment.holdouts[idx], true
}

func (experiment *CompressionExperiment) AddResult(results tools.ExperimentalData) {
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *CompressionExperiment) Outcome() (any, Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *CompressionExperiment) OutcomeForPrompt(idx int) (any, Assertion, any) {
	return tools.EvaluatorOutcomeForPrompt(experiment.evaluator, experiment.tableData, idx)
}

func (experiment *CompressionExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *CompressionExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *CompressionExperiment) Artifacts() []tools.Artifact {
	return CompressionArtifacts(experiment.tableData, experiment.nSamples, experiment.sampleLen)
}

func (experiment *CompressionExperiment) Finalize(substrate any) error {
	rawBytes := experiment.nSamples * experiment.sampleLen

	entries := 0

	for _, row := range experiment.tableData {
		if len(row.Generation) > 0 {
			entries++
		}
	}

	if entries == 0 {
		entries = 1
	}

	ratio := float64(rawBytes) / float64(entries)

	experiment.AddResult(tools.ExperimentalData{
		Idx:  len(experiment.tableData),
		Name: fmt.Sprintf("%d entries from %d KB", entries, rawBytes/1024),
		Scores: tools.Scores{
			Exact:   float64(rawBytes),
			Partial: float64(entries),
			Fuzzy:   ratio,
		},
		WeightedTotal: ratio / (ratio + 1.0),
	})

	return nil
}

var _ tools.HoldoutProvider = (*CompressionExperiment)(nil)
