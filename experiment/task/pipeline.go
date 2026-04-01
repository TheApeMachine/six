package task

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/compute"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/store"
)

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

func (pipeline *Pipeline) Run() (err error) {
	defer pipeline.cancel()

	store.ResetDefaultSpatialIndex()

	loadStart := time.Now()
	backend := compute.NewBackgroundBackend()
	defer func() {
		store.ResetDefaultSpatialIndex()
	}()

	// Grab prompts early so we can size the drop-out buffer.
	prompts := pipeline.experiment.Prompts()
	if len(prompts) == 0 {
		return errnie.Error(PipelineErrNoPrompt)
	}

	if provider, ok := pipeline.experiment.(tools.CorpusProvider); ok {
		if err := pipeline.stageResidentCorpus(backend, provider); err != nil {
			return errnie.Error(err)
		}
	}

	pipeline.timing.loadDur = time.Since(loadStart)

	promptStart := time.Now()

	for idx, prompt := range prompts {
		select {
		case <-pipeline.ctx.Done():
			return errnie.Error(pipeline.ctx.Err())
		default:
		}

		value, newErr := primitive.NewValue([]byte(prompt))

		if newErr != nil {
			pipeline.promptInitFailures.Add(1)
			_ = errnie.Error(newErr)
			continue
		}

		holdout := []byte(nil)

		if provider, ok := pipeline.experiment.(tools.HoldoutProvider); ok {
			if h, ok := provider.HoldoutForPrompt(idx); ok {
				holdout = append([]byte(nil), h...)
			}
		}

		errnie.Trace(
			"experiment.task.pipeline.Run",
			"prompt", prompt,
			"holdout", string(holdout),
		)

		observed, obsErr := pipeline.observePromptValue(backend, value, []byte(prompt), holdout)
		if obsErr != nil {
			return errnie.Error(obsErr)
		}

		store.DefaultSpatialIndex().RemoveValueIDImmediate(value.ID())

		value.InstallFirmware(core.FirmwareTypeTombstone)

		if closeErr := value.Close(); closeErr != nil {
			return errnie.Error(closeErr)
		}

		pipeline.experiment.AddResult(tools.ExperimentalData{
			Idx:      idx,
			Name:     fmt.Sprintf("prompt-%d", idx),
			Prefix:   []byte(prompt),
			Holdout:  holdout,
			Observed: observed,
		})

		pipeline.timing.n++
	}
	pipeline.timing.promptDur = time.Since(promptStart)

	errnie.Trace(
		"experiment.task.pipeline.summary",
		"prompt_init_failures", pipeline.promptInitFailures.Load(),
		"prompts_processed_ok", pipeline.timing.n,
	)

	type finalizer interface {
		Finalize(any) error
	}
	if f, ok := pipeline.experiment.(finalizer); ok {
		finalizeStart := time.Now()
		if err := f.Finalize(nil); err != nil {
			return errnie.Error(err)
		}
		pipeline.timing.finalizeDur = time.Since(finalizeStart)
	}

	if err := pipeline.reporter.WriteResults(pipeline.experiment); err != nil {
		return errnie.Error(err)
	}
	for _, artifact := range pipeline.experiment.Artifacts() {
		if err := pipeline.reporter.WriteArtifact(pipeline.experiment, artifact); err != nil {
			return errnie.Error(err)
		}
	}

	return nil
}

func (pipeline *Pipeline) stageResidentCorpus(backend *compute.Backend, provider tools.CorpusProvider) error {
	if provider == nil {
		return nil
	}

	registrar, _ := pipeline.experiment.(tools.CorpusRegistrar)

	for _, sample := range provider.CorpusSamples() {
		if len(sample) == 0 {
			continue
		}

		value, err := primitive.NewValue(sample)
		if err != nil {
			return err
		}

		if registrar != nil {
			registrar.RegisterCorpusSample(value.ID(), sample)
		}

		value.InstallFirmware(core.FirmwareTypeTombstone)

		if err := value.Close(); err != nil {
			return err
		}

		pipeline.population++
	}

	return nil
}

func (pipeline *Pipeline) observePromptValue(backend *compute.Backend, value *primitive.Value, prompt []byte, holdout []byte) ([]byte, error) {
	if observer, ok := pipeline.experiment.(tools.CorpusObserver); ok {
		return observer.ObserveFromCorpus(prompt, value.ID())
	}

	return pipeline.observePrompt(backend, value, prompt, holdout)
}

func (pipeline *Pipeline) observePrompt(backend *compute.Backend, value *primitive.Value, prompt []byte, holdout []byte) ([]byte, error) {
	if backend == nil {
		return nil, PipelineError("nil compute backend")
	}

	if value == nil {
		return nil, PipelineError("nil prompt value")
	}

	var workspace primitive.Value

	primitive.CopyFrame(&workspace, value)
	workspace.InstallFirmware(core.FirmwareTypeLearn)

	return workspace.Bytes(), nil
}

func (pipeline *Pipeline) inject(observer io.ReadWriter, value *primitive.Value) error {
	if _, err := io.Copy(observer, value); err != nil {
		return err
	}
	pipeline.population++
	return nil
}

func (pipeline *Pipeline) recirculate(observer io.ReadWriter, frame []byte) error {
	_, err := observer.Write(frame)
	return err
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

// PromptInitFailures reports how many prompts failed primitive.NewValue during the last Run.
func (pipeline *Pipeline) PromptInitFailures() int64 {
	if pipeline == nil {
		return 0
	}

	return pipeline.promptInitFailures.Load()
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
