package task

import (
	"bytes"
	"context"
	"fmt"
	"sync/atomic"
	"time"

	tools "github.com/theapemachine/six/experiment"
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
be long enough (see pkg/compute/Backend.gatherCoalesceDuration) or mates never
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

type PipelineError string

const (
	PipelineErrNoPrompt PipelineError = "no prompt values generated"
)

func (e PipelineError) Error() string {
	return string(e)
}

/*
Run is the canonical end-to-end execution path for a paper experiment:

 1. Build a vm.Machine bound to the pipeline's context.
 2. Stream the experiment's dataset through Machine.Load (corpus ingest).
 3. Iterate experiment.Prompts(); for each prompt, encode the text into
    *primitive.Value segments, hand them to Machine.Prompt, and record the
    resulting ExperimentalData row via experiment.AddResult so Score() and
    Outcome() see the same observations the per-prompt assertions do.
 4. Capture per-phase wall-clock timing so the standard summary table reports
    real load/prompt durations instead of zeros.

Run does not write paper artifacts and does not enforce the aggregate gate —
the caller does both, after Run returns nil. Splitting the responsibilities
this way keeps Run pure (deterministic ingest+prompt loop, no FS I/O beyond
the experiment's own Dataset/Reporter) and lets the outer test express the
gate in whatever assertion vocabulary it prefers (Convey, plain testing,
benchmark report, etc.) without re-running the substrate.

Replaces the prior layout where the harness lived in three GoConvey sibling
blocks: in GoConvey each leaf path replays its parent, so the artifact and
gate siblings used to fire against a freshly-constructed pipeline whose
prompt loop never executed — yielding empty tables, zero scores, and
gate-pass telemetry that did not reflect anything the substrate actually
produced.
*/
func (pipeline *Pipeline) Run(ctx context.Context) error {
	if pipeline == nil {
		return PipelineError("Pipeline.Run: nil pipeline")
	}

	if pipeline.experiment == nil {
		return PipelineError("Pipeline.Run: missing experiment")
	}

	machine, err := vm.NewMachine(ctx)
	if err != nil {
		return fmt.Errorf("pipeline.Run: vm.NewMachine: %w", err)
	}
	defer func() { _ = machine.Close() }()

	loadStart := time.Now()
	if err := machine.Load(pipeline.experiment.Dataset()); err != nil {
		return fmt.Errorf("pipeline.Run: machine.Load: %w", err)
	}
	pipeline.timing.loadDur = time.Since(loadStart)

	prompts := pipeline.experiment.Prompts()
	pipeline.timing.n = len(prompts)
	promptStart := time.Now()

	for idx, prompt := range prompts {
		if err := pipeline.runPrompt(machine, idx, prompt); err != nil {
			return err
		}
	}

	pipeline.timing.promptDur = time.Since(promptStart)

	return nil
}

/*
runPrompt encodes one prompt string into *primitive.Value segments, runs them
through vm.Machine.Prompt, and records an ExperimentalData row for downstream
scoring. Failures here are returned (not logged-and-swallowed) because they
make any aggregate score unsound: a missing prompt is a hole in the table,
not a zero-scored row.
*/
func (pipeline *Pipeline) runPrompt(machine *vm.Machine, idx int, prompt string) error {
	holdoutBytes, _ := pipeline.experiment.HoldoutForPrompt(idx)

	rowsBefore, rowsOk := pipelineRowCount(pipeline.experiment)
	if !rowsOk {
		return fmt.Errorf("pipeline.runPrompt[%d]: experiment exposes no row count", idx)
	}

	segments, segErr := primitive.NewValue([]byte(prompt))
	if segErr != nil {
		pipeline.promptInitFailures.Add(1)
		return fmt.Errorf("pipeline.runPrompt[%d]: primitive.NewValue: %w", idx, segErr)
	}

	if len(segments) == 0 {
		pipeline.promptInitFailures.Add(1)
		return fmt.Errorf("pipeline.runPrompt[%d]: %w", idx, PipelineErrNoPrompt)
	}

	resolved, promptErr := machine.Prompt(segments...)
	if promptErr != nil {
		return fmt.Errorf("pipeline.runPrompt[%d]: machine.Prompt: %w", idx, promptErr)
	}

	if len(resolved) == 0 {
		return fmt.Errorf("pipeline.runPrompt[%d]: machine.Prompt returned no resolved values", idx)
	}

	classLabels := make([]string, 0, len(resolved))
	for _, result := range resolved {
		classLabels = append(classLabels, classLabelStringForExperiment(pipeline.experiment, result))
	}

	var classification []byte
	if len(classLabels) > 0 {
		classification = []byte(classLabels[0])
	}

	pipeline.experiment.AddResult(tools.ExperimentalData{
		Idx:               idx,
		Generation:        []byte(resolved[0].String()),
		Holdout:           bytes.Clone(holdoutBytes),
		Prompt:            prompt,
		Segments:          segments,
		Resolved:          resolved,
		ClassLabels:       classLabels,
		Classification:    classification,
		ExecutionSettled:  true,
		ReasoningResolved: true,
	})

	rowsAfter, ok := pipelineRowCount(pipeline.experiment)
	if !ok || rowsAfter <= rowsBefore {
		return fmt.Errorf(
			"pipeline.runPrompt[%d]: result rows did not advance (before=%d after=%d ok=%v)",
			idx, rowsBefore, rowsAfter, ok,
		)
	}

	return nil
}

/*
WriteArtifacts emits the paper-side outputs for the experiment under a single
call so test harnesses do not have to interleave summary, results, and per-
artifact writes themselves. Order matters: the standard summary table reads
the same row set the artifact emitters do, so it is written first to keep
"Wall-clock seconds" in the summary consistent with the row count quoted by
the artifact JSON snapshots that follow.
*/
func (pipeline *Pipeline) WriteArtifacts() error {
	if pipeline == nil || pipeline.experiment == nil || pipeline.reporter == nil {
		return PipelineError("Pipeline.WriteArtifacts: missing experiment or reporter")
	}

	if err := pipeline.writeStandardSummary(); err != nil {
		return fmt.Errorf("pipeline.WriteArtifacts: standard summary: %w", err)
	}

	if err := pipeline.reporter.WriteResults(pipeline.experiment); err != nil {
		return fmt.Errorf("pipeline.WriteArtifacts: results snapshot: %w", err)
	}

	for _, artifact := range pipeline.experiment.Artifacts() {
		if err := pipeline.reporter.WriteArtifact(pipeline.experiment, artifact); err != nil {
			return fmt.Errorf(
				"pipeline.WriteArtifacts: artifact %s (%s): %w",
				artifact.FileName, artifact.Type, err,
			)
		}
	}

	return nil
}

/*
EnforceOutcome runs the experiment's aggregate Outcome() assertion through a
plain comparison and returns a diagnostic string when the gate fails. The
return contract mirrors GoConvey assertion functions: an empty string means
the gate passed, a non-empty string is a human-readable failure message
suitable for t.Fatal / So(message, ShouldBeBlank).
*/
func (pipeline *Pipeline) EnforceOutcome() string {
	if pipeline == nil || pipeline.experiment == nil {
		return "pipeline.EnforceOutcome: missing experiment"
	}

	actual, assertion, threshold := pipeline.experiment.Outcome()
	if assertion == nil {
		return ""
	}

	return assertion(actual, threshold)
}

func pipelineRowCount(experiment tools.PipelineExperiment) (int, bool) {
	rows, ok := experiment.TableData().([]tools.ExperimentalData)
	if !ok {
		return 0, false
	}
	return len(rows), true
}

func classLabelStringForExperiment(experiment tools.PipelineExperiment, value *primitive.Value) string {
	if value == nil {
		return ""
	}

	labelWord, err := value.Property(primitive.LABELS)
	if err != nil {
		return ""
	}

	if named, ok := experiment.(interface{ ClassLabels() []string }); ok {
		names := named.ClassLabels()
		if labelWord > 0 && labelWord <= uint64(len(names)) {
			return names[labelWord-1]
		}
	}

	return fmt.Sprintf("%d", labelWord)
}

func (pipeline *Pipeline) writeStandardSummary() error {
	rows, ok := pipeline.experiment.TableData().([]tools.ExperimentalData)

	if !ok || len(rows) == 0 {
		return nil
	}

	holdoutDesc := ""
	if d, ok := pipeline.experiment.(tools.SummaryHoldoutDescriptor); ok {
		holdoutDesc = d.SummaryHoldoutDescription()
	}

	return WriteStandardSummary(
		pipeline.experiment.Name(),
		pipeline.experiment.Section(),
		rows,
		holdoutDesc,
		pipeline.timing,
	)
}

