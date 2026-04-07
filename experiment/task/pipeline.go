package task

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/viz"
)

var vizOnce sync.Once

type runTiming struct {
	loadDur     time.Duration
	promptDur   time.Duration
	finalizeDur time.Duration
	n           int // number of prompts processed
}

/*
Pipeline is the orchestrator for running experiments.

Current execution path (aligned with the substrate, not the historical
“recirculating machine” sketch):

 1. Build a compute Backend for lifecycle parity with the wider substrate.
 2. For each prompt: NewValue encodes the text into token HV form and
    indexes it in the spatial store (corpus side — separate from readout).
 3. Observe: run the real prompt workspace and read out only what the prompt
    execution produces. Holdout bytes remain supervision metadata for scoring;
    they must not influence Observed directly.
 4. Tombstone the prompt Value and Close it back to the pool.

Paper / eval runs use the same ingress as production vm.Machine: UniversalBitwise
executes each batch, then HomologousCrossover on adjacent same-program pairs.
Prompt frames stall Read until settle; system.evolutionBatchWindow in config must
be long enough (see pkg/compute.Backend.gatherCoalesceDuration) or mates never
share a batch and crossover rarely sees pairs.

A full stream recirculator can be reintroduced later; eval runs must not
pretend it already exists.
*/
type Pipeline struct {
	ctx        context.Context
	cancel     context.CancelFunc
	experiment tools.PipelineExperiment
	scoreWgts  tools.ScoreWeights
	reporter   Reporter
	timing     runTiming
	population int // Tracks the number of resident Values currently in the substrate

	// promptInitFailures counts primitive.NewValue rejections during Run (atomic for safe future concurrent loops).
	promptInitFailures atomic.Int64
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

/*
PipelineWithViz starts the 3D visualization server on the given address
(default ":6600") and activates the global event bus so all kadabra,
compute, and field events stream to the browser in real time.
The server shuts down when the pipeline context is cancelled.
*/
func PipelineWithViz(addr string) pipelineOpts {
	return func(pipeline *Pipeline) {
		vizOnce.Do(func() {
			if addr == "" {
				addr = ":6600"
			}

			server := viz.NewServer(viz.DefaultBus, addr)

			go server.Start(pipeline.ctx)
		})
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
