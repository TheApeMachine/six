package task

import (
	"context"
	"fmt"
	"io"
	"time"
	"unsafe"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/compute"
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

 1. Build a compute Backend for in-band firmware (learn / tombstone).
 2. For each prompt: NewValue encodes the text into token HV form and
    indexes it in the spatial store (corpus side — separate from readout).
 3. Observe: copy the prompt frame, install learn, run UniversalBitwise
    against an identical partner copy (README pairwise learn), then grade
    TokenRegionObservedBytes on the workspace — no Value.String() / exact
    frame-equality LSM readout on the prompt object.
 4. Install tombstone, execute it once on the Backend so regions zero,
    then Close the Value back to the pool.

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

	loadStart := time.Now()
	backend := compute.NewBackend()
	pipeline.timing.loadDur = time.Since(loadStart)
	defer func() {
		backend.Close()
		// Flush any memtables from prompt-time indexing / structure emissions.
		store.DefaultSpatialIndex().Flush()
	}()

	// Grab prompts early so we can size the drop-out buffer.
	prompts := pipeline.experiment.Prompts()
	if len(prompts) == 0 {
		return errnie.Error(PipelineErrNoPrompt)
	}

	promptStart := time.Now()

	for idx, prompt := range prompts {
		select {
		case <-pipeline.ctx.Done():
			return errnie.Error(pipeline.ctx.Err())
		default:
		}

		value, newErr := primitive.NewValue([]byte(prompt))

		if newErr != nil {
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

		observed, obsErr := pipeline.observePrompt(backend, value)
		if obsErr != nil {
			return errnie.Error(obsErr)
		}

		value.InstallTombstone()
		var tombPartner primitive.Value

		if execErr := backend.UniversalBitwise(
			unsafe.Pointer(value),
			unsafe.Pointer(&tombPartner),
		); execErr != nil {
			return errnie.Error(execErr)
		}

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

func (pipeline *Pipeline) observePrompt(backend *compute.Backend, value *primitive.Value) ([]byte, error) {
	if backend == nil {
		return nil, PipelineError("nil compute backend")
	}

	if value == nil {
		return nil, PipelineError("nil prompt value")
	}

	var workSelf, workPartner primitive.Value
	primitive.CopyFrame(&workSelf, value)
	primitive.CopyFrame(&workPartner, value)
	workSelf.InstallLearnFirmware()

	if err := backend.UniversalBitwise(
		unsafe.Pointer(&workSelf),
		unsafe.Pointer(&workPartner),
	); err != nil {
		return nil, err
	}

	// Substrate readout: packed token words after learn — not LSM/String() self-lookup.
	// WorkspaceTokenObserver on an experiment documents that grading expects this path.
	return primitive.TokenRegionObservedBytes(&workSelf), nil
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
