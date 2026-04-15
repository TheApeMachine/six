package task

import (
	"context"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/telemetry"
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
PipelineWithViz is the compatibility entrypoint for telemetry-enabled runs.
The browser-facing bridge now lives under visualizer/; this hook only enables
the Go telemetry publisher path.
*/
func PipelineWithViz(addr string) pipelineOpts {
	return func(_ *Pipeline) {
		vizOnce.Do(func() {
			endpoint := resolveVizTelemetryEndpoint(addr)

			if endpoint == "" {
				telemetry.ConfigureFromConfig()
				return
			}

			errnie.Info("task.PipelineWithViz", "bridge_addr", addr, "telemetry_endpoint", endpoint)
			telemetry.ConfigureUDP(true, endpoint)
		})
	}
}

func resolveVizTelemetryEndpoint(addr string) string {
	configuredEndpoint := ""

	if core.Cfg != nil {
		configuredEndpoint = strings.TrimSpace(core.Cfg.TelemetryEndpoint)
	}

	if strings.TrimSpace(addr) == "" {
		return configuredEndpoint
	}

	host := normalizeVizHost(hostFromAddr(addr))

	if host == "" {
		host = normalizeVizHost(hostFromAddr(configuredEndpoint))
	}

	if host == "" {
		host = "127.0.0.1"
	}

	port := portFromAddr(configuredEndpoint)

	if port == "" {
		port = "8258"
	}

	return net.JoinHostPort(host, port)
}

func hostFromAddr(addr string) string {
	trimmed := strings.TrimSpace(addr)

	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, ":") {
		return ""
	}

	host, _, err := net.SplitHostPort(trimmed)
	if err == nil {
		return host
	}

	return trimmed
}

func portFromAddr(addr string) string {
	trimmed := strings.TrimSpace(addr)

	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, ":") {
		return strings.TrimPrefix(trimmed, ":")
	}

	_, port, err := net.SplitHostPort(trimmed)
	if err != nil {
		return ""
	}

	return port
}

func normalizeVizHost(host string) string {
	switch strings.TrimSpace(host) {
	case "", "0.0.0.0", "::", "[::]":
		return ""
	default:
		return strings.TrimSpace(host)
	}
}

func (pipeline *Pipeline) writeStandardSummary() error {
	rows, ok := pipeline.experiment.TableData().([]tools.ExperimentalData)

	if !ok || len(rows) == 0 {
		return nil
	}

	holdoutDescription := "per dataset configuration"

	if descriptor, typed := pipeline.experiment.(tools.SummaryHoldoutDescriptor); typed {
		holdoutDescription = descriptor.SummaryHoldoutDescription()
	}

	return WriteStandardSummary(
		pipeline.experiment.Name(),
		pipeline.experiment.Section(),
		rows,
		holdoutDescription,
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
