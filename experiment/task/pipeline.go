package task

import (
	"context"
	"fmt"
	"time"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/primitive"
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

	dataset := pipeline.experiment.Dataset()
	pipeline.timing.loadDur = time.Since(loadStart)

	var prompts []data.Prompt
	if promptProvider, ok := dataset.(data.PromptProvider); ok {
		for p := range promptProvider.GeneratePrompts() {
			prompts = append(prompts, p)
		}
	} else {
		rawPrompts := pipeline.experiment.Prompts()
		if len(rawPrompts) == 0 && dataset != nil {
			rawPrompts = promptsFromDataset(dataset)
		}
		for i, p := range rawPrompts {
			prompts = append(prompts, data.Prompt{
				SampleID: uint32(i),
				Text:     p,
			})
		}
	}

	if len(prompts) == 0 {
		return PipelineError("dataset produced zero prompts")
	}

	promptStart := time.Now()
	backend := cpu.NewBackend()

	for idx, prompt := range prompts {
		// The system must run uniformly for each prompt.
		// We project the incoming text into the topological substrate
		// and feed it through the physics engine.
		stateBuf := make([]byte, primitive.ByteSize)
		state := primitive.BytesToValue(stateBuf)

		// Set instruction to OR (0b1110) to accumulate state
		state[primitive.InstrStart>>6] |= (uint64(0b1110) << (primitive.InstrStart & 63))

		for i := 0; i < len(prompt.Text); i++ {
			incoming := primitive.NewValueFromByte(prompt.Text[i])

			// Load incoming data into the worker's operand register
			const dw = primitive.OperandStart >> 6
			const ds = primitive.OperandStart & 63
			for j := 0; j < 4; j++ {
				state[dw+j] |= incoming[j] << ds
				state[dw+j+1] |= incoming[j] >> (64 - ds)
			}
			if (incoming[4] & 1) != 0 {
				state[(primitive.OperandStart+256)>>6] |= 1 << ((primitive.OperandStart + 256) & 63)
			}

			// Run it through the kernel pipeline
			if _, err := backend.Write(stateBuf); err != nil {
				return fmt.Errorf("kernel write failed on prompt %d byte %d: %w", idx, i, err)
			}
		}

		// Extract the final mutated frame (the topological state)
		outFrame := make([]byte, primitive.ByteSize)
		_ = primitive.ValueToBytes(state, outFrame)

		pipeline.experiment.AddResult(tools.ExperimentalData{
			Idx:      idx,
			Name:     pipeline.experiment.Name(),
			Prefix:   []byte(prompt.Text),
			Holdout:  []byte(prompt.Label),
			Observed: outFrame,
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
promptsFromDataset reconstructs full text samples from a dataset's byte stream,
ordered by SampleID, for use as prompts when the experiment does not provide
explicit prompts.
*/
func promptsFromDataset(dataset data.Provider) []string {
	byID := map[byte][]byte{}
	order := []byte{}

	for tok := range dataset.Generate() {
		if _, exists := byID[tok]; !exists {
			order = append(order, tok)
		}

		byID[tok] = append(byID[tok], tok)
	}

	prompts := make([]string, len(order))
	for i, tok := range order {
		prompts[i] = string(tok)
	}

	return prompts
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

	return WriteStandardSummary(
		pipeline.experiment.Name(),
		pipeline.experiment.Section(),
		rows,
		len(rows),
		"",
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
