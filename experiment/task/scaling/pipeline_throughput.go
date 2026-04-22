package scaling

import (
	"fmt"
	"time"

	gc "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
)

/*
PipelineThroughputExperiment measures end-to-end throughput scaling.
Ingests a 200-sample corpus and issues 50 sequential queries, recording
wall-clock timestamps on every AddResult call. This produces per-query
latency data that reveals how query cost scales with cumulative load.
*/
type PipelineThroughputExperiment struct {
	tableData    []tools.ExperimentalData
	dataset      data.Provider
	prompt       []string
	holdouts     [][]byte
	ingestTime   time.Time
	promptStamps []time.Time
	sampleLen    int
	nSamples     int
	evaluator    *tools.Evaluator
}

func NewPipelineThroughputExperiment() *PipelineThroughputExperiment {
	return &PipelineThroughputExperiment{
		tableData: []tools.ExperimentalData{},
		prompt:    []string{},
		dataset:   NewSyntheticDataset(128, 200, 42),
		sampleLen: 128,
		nSamples:  200,
		evaluator: tools.NewEvaluator(
			tools.EvalWithExpectation(0.05, 0.50),
		),
	}
}

func (experiment *PipelineThroughputExperiment) Name() string    { return "Pipeline Throughput" }
func (experiment *PipelineThroughputExperiment) Section() string { return "scaling" }
func (experiment *PipelineThroughputExperiment) Dataset() data.Provider {
	return experiment.dataset
}

func (experiment *PipelineThroughputExperiment) Prompts() []string {
	experiment.ingestTime = time.Now()

	ds, ok := experiment.dataset.(*SyntheticDataset)
	if !ok {
		if experiment.prompt == nil {
			experiment.prompt = []string{}
		} else {
			experiment.prompt = experiment.prompt[:0]
		}

		experiment.holdouts = nil
		return experiment.prompt
	}

	experiment.prompt, experiment.holdouts = syntheticSamplePrompts(ds, 50, 32)
	return experiment.prompt
}

func (experiment *PipelineThroughputExperiment) HoldoutForPrompt(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(experiment.holdouts) {
		return nil, false
	}

	return experiment.holdouts[idx], true
}

func (experiment *PipelineThroughputExperiment) AddResult(results tools.ExperimentalData) {
	experiment.promptStamps = append(experiment.promptStamps, time.Now())
	experiment.evaluator.Enrich(&results)
	experiment.tableData = append(experiment.tableData, results)
}

func (experiment *PipelineThroughputExperiment) Outcome() (any, gc.Assertion, any) {
	return experiment.evaluator.Outcome(experiment.Score())
}

func (experiment *PipelineThroughputExperiment) OutcomeForPrompt(idx int) (any, gc.Assertion, any) {
	return experiment.evaluator.OutcomeForPromptConvey(experiment.tableData, idx)
}

func (experiment *PipelineThroughputExperiment) Score() float64 {
	return experiment.evaluator.MeanScore(experiment.tableData)
}

func (experiment *PipelineThroughputExperiment) TableData() any {
	return experiment.tableData
}

func (experiment *PipelineThroughputExperiment) Artifacts() []tools.Artifact {
	return ThroughputArtifacts(experiment.tableData, experiment.ingestTime, experiment.promptStamps)
}

func (experiment *PipelineThroughputExperiment) RawOutput() bool { return false }

func (experiment *PipelineThroughputExperiment) Finalize(substrate any) error {
	elapsed := time.Since(experiment.ingestTime)
	totalBytes := experiment.nSamples * experiment.sampleLen

	entries := 0

	for _, row := range experiment.tableData {
		if len(row.Generation) > 0 {
			entries++
		}
	}

	if entries == 0 {
		entries = 1
	}

	kbPerSec := 0.0
	if elapsed.Milliseconds() > 0 {
		kbPerSec = (float64(totalBytes) / 1024.0) / (float64(elapsed.Milliseconds()) / 1000.0)
	}

	experiment.AddResult(tools.ExperimentalData{
		Idx:  len(experiment.tableData),
		Name: fmt.Sprintf("Summary: %d entries, %.0f KB/s", entries, kbPerSec),
		Scores: tools.Scores{
			Exact:   kbPerSec,
			Partial: float64(entries),
			Fuzzy:   float64(elapsed.Milliseconds()),
		},
		WeightedTotal: kbPerSec / (kbPerSec + 100.0),
	})

	return nil
}

/*
PerPromptLatencies returns per-query wall-clock durations in milliseconds.
The first entry measures time from Prompts() return (ingest complete) to
the first AddResult; subsequent entries measure inter-query gaps.
*/
func (experiment *PipelineThroughputExperiment) PerPromptLatencies() []float64 {
	n := len(experiment.promptStamps)
	if n == 0 {
		return nil
	}

	latencies := make([]float64, n)
	latencies[0] = float64(experiment.promptStamps[0].Sub(experiment.ingestTime).Milliseconds())

	for i := 1; i < n; i++ {
		latencies[i] = float64(experiment.promptStamps[i].Sub(experiment.promptStamps[i-1]).Milliseconds())
	}

	return latencies
}

var _ tools.HoldoutProvider = (*PipelineThroughputExperiment)(nil)

