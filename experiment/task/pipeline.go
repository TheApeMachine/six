package task

import (
	"context"
	"errors"
	"io"
	"strings"
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
	defer func() {
		if closeErr := machine.Close(); closeErr != nil {
			_ = errnie.Error(closeErr, "op", "pipeline.Run.machine.Close")
			if err == nil {
				err = closeErr
			} else {
				err = errors.Join(err, closeErr)
			}
		}
	}()

	// ── Recirculation loop ───────────────────────────────────────────
	// Read executed frames from the pipe output and feed them back into
	// the pipe input. Each pass through the Stream pairs the frame with
	// its neighbor and fires the ALU. The fan-out in Stream.Write
	// deposits every result into Regions, so the population accumulates
	// there. The loop runs until CloseWrite shuts the pipe.
	loopDone := make(chan error, 1)
	go func() {
		buf := make([]byte, primitive.ByteSize)
		for {
			if _, err := machine.Read(buf); err != nil {
				loopDone <- err
				return
			}
			if _, err := machine.Write(buf); err != nil {
				loopDone <- err
				return
			}
		}
	}()

	// ── Phase 1: Hydrate ────────────────────────────────────────────
	// Pump dataset into the Machine. Frames enter the recirculation
	// loop and begin mixing immediately.
	if ds := pipeline.experiment.Dataset(); ds != nil {
		if _, copyErr := io.Copy(machine, ds); copyErr != nil {
			return errnie.Error(copyErr)
		}
	}

	// ── Phase 2: Prompt ─────────────────────────────────────────────
	// Inject workspace Values carrying Viral firmware. They enter the
	// same recirculation loop and propagate through the population.
	prompts := pipeline.experiment.Prompts()
	for idx, prompt := range prompts {
		errnie.Trace("Prompt", "prompt", prompt)

		workspace, werr := primitive.NewValue([]byte(prompt))
		if werr != nil {
			return errnie.Error(werr)
		}
		workspace[core.Cfg.FW] = uint64(core.FirmwareTypeViral)

		frame := make([]byte, primitive.ByteSize)
		if err := primitive.ValueToBytes(workspace, frame); err != nil {
			return errnie.Error(err)
		}

		if _, err = machine.Write(frame); err != nil {
			return errnie.Error(err)
		}

		errnie.Debug("Prompt", "prompt", prompt, "idx", idx)
	}

	// ── Phase 3: Stop and observe ───────────────────────────────────
	// Close the pipe to stop recirculation, then read results from
	// Regions where every pass deposited frames via fan-out.
	if cwErr := machine.CloseWrite(); cwErr != nil {
		return errnie.Error(cwErr)
	}
	if loopErr := <-loopDone; loopErr != nil && !errors.Is(loopErr, io.EOF) {
		return errnie.Error(loopErr)
	}

	// Collect observations from Regions.
	readBuf := make([]byte, primitive.ByteSize)
	for idx, prompt := range prompts {
		// Read one frame from the first Region that has data.
		found := false
		for _, reg := range machine.Regions() {
			if _, err := reg.Read(readBuf); err == nil {
				found = true
				break
			}
		}

		obs := make([]byte, primitive.ByteSize)
		if found {
			copy(obs, readBuf)
		}

		observed := obs
		if wo, ok := pipeline.experiment.(tools.WorkspaceTokenObserver); ok && wo.ObserveWorkspaceAsTokens() {
			var result primitive.Value
			if err := result.ApplyWireFrame(obs); err == nil {
				observed = []byte(strings.TrimSpace(primitive.DecodeTokensToText(&result)))
			}
		}

		holdout := []byte(nil)
		if hp, ok := pipeline.experiment.(tools.HoldoutProvider); ok {
			if h, ok := hp.HoldoutForPrompt(idx); ok {
				holdout = h
			}
		}

		pipeline.experiment.AddResult(tools.ExperimentalData{
			Idx:      idx,
			Name:     pipeline.experiment.Name(),
			Prefix:   []byte(prompt),
			Holdout:  holdout,
			Observed: observed,
		})
	}

	if err := pipeline.reporter.WriteResults(pipeline.experiment); err != nil {
		return err
	}

	for _, artifact := range pipeline.experiment.Artifacts() {
		if err := pipeline.reporter.WriteArtifact(pipeline.experiment, artifact); err != nil {
			return err
		}
	}

	if err := pipeline.writeStandardSummary(); err != nil {
		return errnie.Error(err)
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
