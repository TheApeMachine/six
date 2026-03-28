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

1. Read data from the dataset to create Values in the system
2. Deploy any needed Values with programs (e.g. Affinity, etc.)
3. Deploy Values with prompt programs (bytes folded through workspace; firmware from config)
4. Observe results: raw Value frame by default, or token-region text when the experiment implements WorkspaceTokenObserver (answers materialized in tokens)
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

	// Hydrate the region mesh from the dataset (transport only — no ALU here).
	// The stream pipe is bounded; a concurrent drain avoids blocking once the
	// ring fills. CloseWrite after Copy lets the drain exit on EOF.
	if ds := pipeline.experiment.Dataset(); ds != nil {
		drainDone := make(chan error, 1)
		go func() {
			_, err := io.Copy(io.Discard, machine)
			drainDone <- err
		}()

		_, copyErr := io.Copy(machine, ds)
		cwErr := machine.CloseWrite()

		drainErr := <-drainDone
		if copyErr != nil {
			return errnie.Error(copyErr)
		}
		if cwErr != nil {
			return errnie.Error(cwErr)
		}
		if drainErr != nil && !errors.Is(drainErr, io.EOF) {
			return errnie.Error(drainErr)
		}
	}

	// Always-on workspace: folding happens only via Value.Write → Backend (fold-through).
	workspace, werr := primitive.NewValue(nil)
	if werr != nil {
		return errnie.Error(werr)
	}
	workspace[core.Cfg.FW] = uint64(core.FirmwareTypeViral)
	defer workspace.Close()

	prompts := pipeline.experiment.Prompts()
	for idx, prompt := range prompts {
		errnie.Trace("Prompt", "prompt", prompt)

		if _, err = io.Copy(workspace, strings.NewReader(prompt)); err != nil {
			return errnie.Error(err)
		}

		var obs []byte
		if wo, ok := pipeline.experiment.(tools.WorkspaceTokenObserver); ok && wo.ObserveWorkspaceAsTokens() {
			obs = []byte(strings.TrimSpace(primitive.DecodeTokensToText(workspace)))
		} else {
			obs = make([]byte, primitive.ByteSize)
			if err := primitive.ValueToBytes(workspace, obs); err != nil {
				return errnie.Error(err)
			}
		}

		errnie.Debug("Prompt", "prompt", prompt)

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
			Observed: obs,
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
