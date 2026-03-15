package task

import (
	"context"
	"time"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/vm"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

const pipelineDrainTimeout = 2 * time.Second

type promptFailure struct {
	idx      int
	prompt   string
	expected string
	got      string
}

type runTiming struct {
	loadDur     time.Duration
	promptDur   time.Duration
	finalizeDur time.Duration
	n           int // number of prompts processed
}

type Pipeline struct {
	ctx       context.Context
	cancel    context.CancelFunc
	machine   *vm.Machine
	experiment tools.PipelineExperiment
	scoreWgts tools.ScoreWeights
	reporter  Reporter
	timing    runTiming
}

type pipelineOpts func(*Pipeline)

func NewPipeline(opts ...pipelineOpts) (*Pipeline, error) {
	ctx, cancel := context.WithCancel(context.Background())

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

	promptStart := time.Now()

	for idx, prompt := range prompts {
		result, err := pipeline.machine.Prompt(prompt)
		if err != nil {
			return err
		}

		pipeline.experiment.AddResult(tools.ExperimentalData{
			Idx:      idx,
			Name:     pipeline.experiment.Name(),
			Prefix:   []byte(prompt),
			Holdout:  []byte(result),
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
	PipelineErrNoPrompt PipelineError = "no prompt chords generated"
)

func (e PipelineError) Error() string {
	return string(e)
}
