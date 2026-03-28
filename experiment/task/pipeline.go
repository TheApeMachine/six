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
	machine, err := vm.NewMachine(
		vm.WithContext(pipeline.ctx),
		vm.WithDataset(pipeline.experiment.Dataset()),
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

	if err := pipeline.maybeSeedViralLearn(observer); err != nil {
		return errnie.Error(err)
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

		value[core.Cfg.StateIndex] = 1

		// Inject the prompt frame, then read the transformed frame back once.
		observedFrame, err := pipeline.injectAndObserve(observer, value)
		if err != nil {
			return errnie.Error(err)
		}

		observedValue := primitive.BytesToValue(observedFrame)
		observedText := observedValue.String()

		if tokenObserver, ok := pipeline.experiment.(tools.WorkspaceTokenObserver); ok && tokenObserver.ObserveWorkspaceAsTokens() {
			observedText = primitive.DecodeTokensToText(observedValue)
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
			"value", observedText,
		)

		pipeline.experiment.AddResult(tools.ExperimentalData{
			Idx:      idx,
			Name:     fmt.Sprintf("prompt-%d", idx),
			Prefix:   []byte(prompt),
			Holdout:  holdout,
			Observed: []byte(observedText),
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

	machine.Close()

	return nil
}

func (pipeline *Pipeline) injectAndObserve(observer io.ReadWriter, value *primitive.Value) ([]byte, error) {
	if _, err := io.Copy(observer, value); err != nil {
		return nil, err
	}

	observedFrame := make([]byte, primitive.ByteSize)
	if _, err := io.ReadFull(observer, observedFrame); err != nil {
		return nil, err
	}

	return observedFrame, nil
}

func (pipeline *Pipeline) hydrateDataset(observer io.ReadWriter) error {
	dataset := pipeline.experiment.Dataset()
	if dataset == nil {
		return nil
	}

	// NewValue stores one tokenized byte per 64-bit token word.
	chunkSize := int((core.Cfg.TokenBits + 63) / 64)
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
		frame[core.Cfg.StateIndex] = 1

		if _, err := pipeline.injectAndObserve(observer, frame); err != nil {
			return err
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

func (pipeline *Pipeline) maybeSeedViralLearn(observer io.ReadWriter) error {
	seeder, ok := pipeline.experiment.(viralLearnSeeder)
	if !ok || !seeder.SeedViralLearn() {
		return nil
	}

	seed, err := primitive.NewValue(nil)
	if err != nil {
		return err
	}
	seed[core.Cfg.StateIndex] = 1
	seed[core.Cfg.RegPC] = 0
	seed[core.Cfg.FW] = 0

	program := append([]uint32(nil), core.Cfg.Firmware[core.FirmwareTypeViral]...)
	program = append(program, encodeWriteRegImmediate(uint16(core.FirmwareTypeLearn), core.Cfg.FW))
	installProgram(seed, core.Cfg.ProgramIndex, program)

	_, err = pipeline.injectAndObserve(observer, seed)
	return err
}

func installProgram(value *primitive.Value, wordStart int, program []uint32) {
	for i, w := 0, uint64(wordStart); i < len(program) && int(w) < primitive.Words; i, w = i+2, w+1 {
		v := uint64(program[i])
		if i+1 < len(program) {
			v |= uint64(program[i+1]) << 32
		}
		value[w] = v
	}
}

func encodeWriteRegImmediate(src uint16, dstReg int) uint32 {
	const (
		opA                 = uint32(0x3)
		operandFlagRegister = uint16(0x3000)
	)
	sc := uint32(src & 0x3FFF)
	dc := uint32((operandFlagRegister | uint16(dstReg)) & 0x3FFF)
	return opA | (sc << 4) | (dc << 18)
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
