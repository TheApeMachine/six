package task

import (
	"context"
	"fmt"
	"io"
	"time"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/vm"
)

type runTiming struct {
	loadDur     time.Duration
	promptDur   time.Duration
	finalizeDur time.Duration
	n           int // number of prompts processed
}

/*
Pipeline is the orchestrator for running experiments.
The Six architecture is "always-on" so this needs to
be orchestrated in a particular way.

 1. Start the recirculation loop: a goroutine reads executed frames
    from the pipe's output and writes them back into the pipe's input.
    Each pass through the Stream pairs the frame with its neighbor and
    fires the ALU. Values keep mixing until the pipe is closed.
 2. Hydrate: pump dataset bytes into the Machine. They enter the
    recirculation loop and begin mixing immediately.
 3. Prompt: inject workspace Values carrying Viral firmware into the
    Machine. They enter the same loop and propagate through the
    population via firmware chaining.
 4. Observe: after all prompts are injected, close the pipe to stop
    recirculation, then read results from the Regions (which
    accumulated every frame via fan-out on each pass).
*/
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

func (pipeline *Pipeline) Run() (err error) {
	machine, err := vm.NewMachine(
		vm.WithContext(pipeline.ctx),
		vm.WithDataset(pipeline.experiment.Dataset()),
	)

	if err != nil {
		return errnie.Error(err)
	}

	go func() {
		observer := tools.NewObserver(machine)
		defer observer.Close()

		for {
			select {
			case <-pipeline.ctx.Done():
				return
			default:
				io.Copy(observer, observer)
			}
		}
	}()

	// Grab prompts early so we can size the drop-out buffer.
	prompts := pipeline.experiment.Prompts()

	for idx, prompt := range prompts {
		value, err := primitive.NewValue([]byte(prompt))

		if err != nil {
			errnie.Error(err)
			continue
		}

		// Inject prompt
		io.Copy(machine, value)

		for {
			select {
			case <-pipeline.ctx.Done():
				return errnie.Error(pipeline.ctx.Err())
			default:
				// Check if the prompt Value has tokens.
				if value.String() == "" {
					continue
				}

				errnie.Trace(
					"experiment.task.pipeline.Run",
					"prompt", prompt,
					"value", value.String(),
				)

				pipeline.experiment.AddResult(tools.ExperimentalData{
					Idx:      idx,
					Name:     fmt.Sprintf("prompt-%d", idx),
					Prefix:   []byte(prompt),
					Observed: []byte(value.String()),
				})

				break
			}
		}
	}

	return nil
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
