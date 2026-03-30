package task

import (
	"context"
	"fmt"
	"io"
	"time"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/core"
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

type viralLearnSeeder interface {
	SeedViralLearn() bool
}

type seedValueProvider interface {
	SeedValues() []*primitive.Value
}

/*
Pipeline is the orchestrator for running experiments.
The Six architecture is "always-on" so this needs to
be orchestrated in a particular way.

 1. Start the recirculation loop: pump executed frames from the pipe's
    output back into the pipe's input. Each pass through the Stream
    pairs a resident wait Value with the next partner and fires the ALU.
    Values keep mixing until the pipe is closed.
 2. Hydrate: pump dataset bytes into the Machine. They enter the
    recirculation loop and begin mixing immediately.
    Inject additional "conditioning" programs, such as the Affinity
    firmware, which needs to be distributed. The way to distribute things
    is to load the Viral firmware, and set the final two instructions
    to FW=<affinity> and PC=<start of bootloader>, which results in a
    Value that will first copy the Viral firmware into another Value,
    and then load and execute the Affinity firmware.
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
	population int // Tracks the number of resident Values currently in the substrate
	config     vm.SubstrateConfig
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

	if pipeline.config.ValueConfig == nil {
		pipeline.config.ValueConfig = core.Cfg
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
	machine, err := vm.NewMachine(
		pipeline.ctx,
		vm.WithDataset(pipeline.experiment.Dataset()),
		vm.WithConfig(pipeline.config),
	)

	if err != nil {
		return errnie.Error(err)
	}
	pipeline.timing.loadDur = time.Since(loadStart)
	defer func() {
		if closeErr := machine.Close(); closeErr != nil && err == nil {
			err = errnie.Error(closeErr)
		}
	}()

	observer := tools.NewObserver(machine)
	defer observer.Close()

	// Grab prompts early so we can size the drop-out buffer.
	prompts := pipeline.experiment.Prompts()
	if len(prompts) == 0 {
		return errnie.Error(PipelineErrNoPrompt)
	}

	if err := pipeline.hydrateDataset(observer); err != nil {
		return errnie.Error(err)
	}

	// Recirculation loop: pump the stream to allow the dataset and seed to mix and evolve.
	// We pump based on the size of the stream/dataset to ensure full mixing rather than a hardcoded 2000.
	pumpCount := pipeline.population * 2 // Apply a 2x mixing multiplier to hydrated data.
	if pumpCount == 0 {
		pumpCount = 100 // fallback if no dataset was hydrated
	}

	for i := 0; i < pumpCount; i++ {
		buf := make([]byte, primitive.ByteSize)
		if _, err := observer.Read(buf); err != nil {
			break
		}
		if err := pipeline.recirculate(observer, buf); err != nil {
			return errnie.Error(err)
		}
	}

	promptStart := time.Now()
	for idx, prompt := range prompts {
		select {
		case <-pipeline.ctx.Done():
			return errnie.Error(pipeline.ctx.Err())
		default:
		}

		value, err := primitive.NewValue([]byte(prompt))

		if err != nil {
			_ = errnie.Error(err)
			continue
		}

		value[pipeline.config.ValueConfig.StateIndex] = 1

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

		pipeline.experiment.AddResult(tools.ExperimentalData{
			Idx:      idx,
			Name:     fmt.Sprintf("prompt-%d", idx),
			Prefix:   []byte(prompt),
			Holdout:  holdout,
			Observed: []byte(value.String()),
		})

		pipeline.timing.n++
	}
	pipeline.timing.promptDur = time.Since(promptStart)

	type finalizer interface {
		Finalize(any) error
	}
	if f, ok := pipeline.experiment.(finalizer); ok {
		finalizeStart := time.Now()
		if err := f.Finalize(machine); err != nil {
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

func (pipeline *Pipeline) hydrateDataset(observer io.ReadWriter) error {
	dataset := pipeline.experiment.Dataset()
	if dataset == nil {
		return nil
	}

	// NewValue stores one tokenized byte per 64-bit token word.
	chunkSize := int((pipeline.config.ValueConfig.TokenBits + 63) / 64)
	if chunkSize <= 0 {
		return nil
	}

	chunk := make([]byte, 0, chunkSize)
	flush := func() error {
		if len(chunk) == 0 {
			return nil
		}

		frame, err := primitive.NewValue(append([]byte(nil), chunk...))
		if err != nil {
			return err
		}
		frame[pipeline.config.ValueConfig.StateIndex] = 1

		if err := pipeline.inject(observer, frame); err != nil {
			return err
		}
		closeErr := frame.Close()
		if closeErr != nil {
			return closeErr
		}

		chunk = chunk[:0]
		return nil
	}

	for b := range dataset.Generate() {
		chunk = append(chunk, b)
		if len(chunk) == chunkSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}

	return flush()
}

func (pipeline *Pipeline) maybeSeedValues(observer io.ReadWriter) error {
	provider, ok := pipeline.experiment.(seedValueProvider)
	if !ok {
		return nil
	}

	for _, seed := range provider.SeedValues() {
		if seed == nil {
			continue
		}
		if idx := pipeline.config.ValueConfig.StateIndex; idx >= 0 && idx < len(seed) && seed[idx] == 0 {
			seed[idx] = 1
		}
		if err := pipeline.inject(observer, seed); err != nil {
			_ = seed.Close()
			return err
		}
		if err := seed.Close(); err != nil {
			return err
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

func PipelineWithConfig(config vm.SubstrateConfig) pipelineOpts {
	return func(pipeline *Pipeline) {
		pipeline.config = config
	}
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
