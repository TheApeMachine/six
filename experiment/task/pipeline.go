package task

import (
	"context"
	"time"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

type runTiming struct {
	loadDur     time.Duration
	promptDur   time.Duration
	finalizeDur time.Duration
	n           int // number of prompts processed
}

type Pipeline struct {
	ctx        context.Context
	cancel     context.CancelFunc
	machine    *vm.Machine
	experiment tools.PipelineExperiment
	scoreWgts  tools.ScoreWeights
	reporter   Reporter
	timing     runTiming
}

type pipelineOpts func(*Pipeline)

func NewPipeline(ctx context.Context, opts ...pipelineOpts) (*Pipeline, error) {
	ctx, cancel := context.WithCancel(ctx)

	pipeline := &Pipeline{
		ctx:       ctx,
		cancel:    cancel,
		scoreWgts: tools.DefaultScoreWeights(),
	}

	for _, opt := range opts {
		opt(pipeline)
	}

	if pipeline.experiment == nil {
		return nil, PipelineError(
			"missing experiment: use PipelineWithExperiment",
		)
	}

	if pipeline.reporter == nil {
		pipeline.reporter = NewProjectorReporter()
	}

	return pipeline, nil
}

func (pipeline *Pipeline) Run() error {
	defer pipeline.cancel()

	loadStart := time.Now()

	pipeline.machine = vm.NewMachine(
		vm.MachineWithContext(pipeline.ctx),
	)
	defer pipeline.machine.Close()

	dataset := pipeline.experiment.Dataset()
	if dataset != nil {
		if err := pipeline.machine.SetDataset(dataset); err != nil {
			return err
		}
	}

	pipeline.timing.loadDur = time.Since(loadStart)

	prompts := pipeline.experiment.Prompts()

	if len(prompts) == 0 && dataset != nil {
		prompts = promptsFromDataset(dataset)
	}

	if len(prompts) == 0 {
		return PipelineError("dataset produced zero prompts")
	}

	holdoutN, holdoutType := pipeline.experiment.Holdout()

	promptStart := time.Now()

	for idx, prompt := range prompts {
		prefix, expected := splitHoldout(prompt, holdoutN, holdoutType)

		result, err := pipeline.machine.Prompt(prefix)
		if err != nil {
			return err
		}

		pipeline.experiment.AddResult(tools.ExperimentalData{
			Idx:      idx,
			Name:     pipeline.experiment.Name(),
			Prefix:   []byte(prefix),
			Holdout:  expected,
			Observed: result,
		})
	}

	pipeline.timing.promptDur = time.Since(promptStart)
	pipeline.timing.n = len(prompts)

	finalizeStart := time.Now()

	if err := pipeline.reporter.WriteResults(pipeline.experiment); err != nil {
		return err
	}

	for _, artifact := range pipeline.experiment.Artifacts() {
		if err := pipeline.reporter.WriteArtifact(pipeline.experiment, artifact); err != nil {
			return err
		}
	}

	pipeline.timing.finalizeDur = time.Since(finalizeStart)

	return pipeline.writeStandardSummary()
}

/*
splitHoldout separates a prompt into the prefix the machine sees and
the expected bytes it must reconstruct. The holdoutN parameter is
interpreted as a byte count for RIGHT/LEFT/CENTER, or ignored for
MATCH (which strips label substrings). Returns the truncated prefix
and the held-out ground truth.
*/
func splitHoldout(prompt string, holdoutN int, holdoutType input.HoldoutType) (string, []byte) {
	raw := []byte(prompt)

	if holdoutType == input.NONE || holdoutN <= 0 || len(raw) == 0 {
		return prompt, nil
	}

	n := min(holdoutN, len(raw))

	switch holdoutType {
	case input.RIGHT:
		cut := len(raw) - n
		return string(raw[:cut]), raw[cut:]

	case input.LEFT:
		return string(raw[n:]), raw[:n]

	case input.CENTER:
		start := (len(raw) - n) / 2
		expected := make([]byte, n)
		copy(expected, raw[start:start+n])

		prefix := make([]byte, 0, len(raw)-n)
		prefix = append(prefix, raw[:start]...)
		prefix = append(prefix, raw[start+n:]...)

		return string(prefix), expected

	case input.MATCH:
		return prompt, nil
	}

	return prompt, nil
}

/*
promptsFromDataset reconstructs full text samples from a dataset's RawToken
stream, ordered by SampleID, for use as prompts when the experiment does not
provide explicit prompts.
*/
func promptsFromDataset(dataset provider.Dataset) []string {
	byID := map[uint32][]byte{}
	order := []uint32{}

	for tok := range dataset.Generate() {
		if _, exists := byID[tok.SampleID]; !exists {
			order = append(order, tok.SampleID)
		}

		byID[tok.SampleID] = append(byID[tok.SampleID], tok.Symbol)
	}

	prompts := make([]string, 0, len(order))

	for _, id := range order {
		prompts = append(prompts, string(byID[id]))
	}

	return prompts
}

func extractScores(data []tools.ExperimentalData, field string) []float64 {
	scores := make([]float64, len(data))

	for i, d := range data {
		switch field {
		case "Exact":
			scores[i] = d.Scores.Exact
		case "Partial":
			scores[i] = d.Scores.Partial
		case "Fuzzy":
			scores[i] = d.Scores.Fuzzy
		case "Weighted":
			scores[i] = d.WeightedTotal
		}
	}

	return scores
}

func PipelineWithExperiment(experiment tools.PipelineExperiment) pipelineOpts {
	return func(pipeline *Pipeline) {
		pipeline.experiment = experiment
	}
}

func PipelineWithScoreWeights(weights tools.ScoreWeights) pipelineOpts {
	return func(pipeline *Pipeline) {
		pipeline.scoreWgts = weights
	}
}

func PipelineWithReporter(reporter Reporter) pipelineOpts {
	return func(pipeline *Pipeline) {
		pipeline.reporter = reporter
	}
}

func PipelineWithSnapshotReporter() pipelineOpts {
	return func(pipeline *Pipeline) {
		pipeline.reporter = NewSnapshotReporter()
	}
}

func (pipeline *Pipeline) writeStandardSummary() error {
	rows, ok := pipeline.experiment.TableData().([]tools.ExperimentalData)
	if !ok || len(rows) == 0 {
		return nil
	}

	holdoutN, holdoutType := pipeline.experiment.Holdout()

	htStr := "RIGHT"
	switch holdoutType {
	case input.LEFT:
		htStr = "LEFT"
	case input.CENTER:
		htStr = "CENTER"
	case input.RANDOM:
		htStr = "RANDOM"
	case input.MATCH:
		htStr = "MATCH"
	}

	return WriteStandardSummary(
		pipeline.experiment.Name(),
		pipeline.experiment.Section(),
		rows,
		holdoutN,
		htStr,
		pipeline.timing,
	)
}

type PipelineError string

const (
	PipelineErrNoPrompt PipelineError = "no prompt values generated"
)

func (e PipelineError) Error() string {
	return string(e)
}
