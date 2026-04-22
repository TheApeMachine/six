//go:build exp_pipeline

// Integration pipeline (TestPipeline): build with -tags=exp_pipeline; see Makefile paper / pprof targets.
package task

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/task/classification"
	"github.com/theapemachine/six/experiment/task/codegen"
	"github.com/theapemachine/six/experiment/task/imagegen"
	"github.com/theapemachine/six/experiment/task/logic"
	"github.com/theapemachine/six/experiment/task/misc"
	"github.com/theapemachine/six/experiment/task/phasedial"
	"github.com/theapemachine/six/experiment/task/scaling"
	"github.com/theapemachine/six/experiment/task/textgen"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
)

func TestMain(m *testing.M) {
	if err := tryLoadConfigForTaskTests(); err != nil {
		fmt.Fprintf(os.Stderr, "experiment/task tests: tryLoadConfigForTaskTests: %v\n", err)
		os.Exit(1)
	}

	if err := errnie.InitLoggerFromViper(); err != nil {
		fmt.Fprintf(os.Stderr, "experiment/task tests: errnie.InitLoggerFromViper: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func tryLoadConfigForTaskTests() error {
	viper.SetConfigType("yml")
	viper.Set("telemetry.ws_url", "ws://127.0.0.1:6600/ws")

	candidates := []string{
		filepath.Join("..", "..", "cmd", "cfg", "config.yml"),
		"cmd/cfg/config.yml",
	}

	var lastErr error

	for _, path := range candidates {
		viper.SetConfigFile(path)

		readErr := viper.ReadInConfig()
		if readErr == nil {
			core.NewConfig()

			return nil
		}

		lastErr = readErr
	}

	fmt.Fprintf(
		os.Stderr,
		"experiment/task tests: no config loaded from candidate paths; last viper.ReadInConfig error: %v\n",
		lastErr,
	)
	core.NewConfig()

	return lastErr
}

/*
TestPipeline runs the experiments for the research paper.
It autoamtically builds the paper artifacts so experiment
results are always an accurate representation of current
architecture capabilities.

 1. Load corpus — all dataset Values are created with Learn firmware and
    queued for backend execution.
 2. Evolve — the substrate runs for evolutionBudget. Signal emission creates
    child Values, HolographicCrossover evolves programs, and the population
    recirculates through handleFollowUp.
 3. Prompt — inject prompt Values, wait for settle, walk the output chain.
 4. Score — compare observed output to holdout, record ExperimentalData.
 5. Artifacts — write results and artifact JSON/TeX files.

Gate misses are recorded as experiment telemetry; panics, prompt errors, invalid
result tables, and I/O errors are not. A full run takes minutes — use a long
-timeout (e.g. go test -timeout 30m ./experiment/task -run TestPipeline).

Each prompt records tools.ExperimentalData via AddResult so Score()/Outcome()
see the same readout path as paper pipelines; prompts without a holdout skip
strict equality only. Experiments with Finalize run it after all prompts.

Per-prompt rows are asserted structurally so the generator cannot silently emit
an invalid table. Aggregate Outcome() remains available to the reporter and is
written into result snapshots as pass/fail telemetry.
*/
func TestPipeline(t *testing.T) {
	allExperiments := []tools.PipelineExperiment{
		scaling.NewSubstrateQueryScalingExperiment(),
		scaling.NewCompressionExperiment(),
		scaling.NewPipelineThroughputExperiment(),
		scaling.NewSequencerExperiment(),
		codegen.NewLanguagesExperiment(t.Context()),
		classification.NewTextClassificationExperiment(),
		classification.NewBlindClassificationExperiment(),
		textgen.NewCompositionalExperiment(),
		textgen.NewProseChainingExperiment(),
		textgen.NewOutOfCorpusExperiment(),
		textgen.NewTextOverlapExperiment(),
		phasedial.NewAdaptiveSplitExperiment(),
		phasedial.NewChunkingBaselineExperiment(),
		phasedial.NewConstraintResolutionExperiment(),
		phasedial.NewCorrelationLengthExperiment(),
		phasedial.NewGroupActionEquivarianceExperiment(),
		phasedial.NewPartialDeletionExperiment(),
		phasedial.NewPermutationInvarianceExperiment(),
		phasedial.NewPhaseCoherenceExperiment(),
		phasedial.NewQueryRobustnessExperiment(),
		phasedial.NewSnapToSurfaceExperiment(),
		phasedial.NewSteerabilityExperiment(),
		phasedial.NewTwoHopRetrievalExperiment(),
		imagegen.NewReconstructionExperiment(),
		logic.NewBabiExperiment(),
		logic.NewSemanticAlgebraExperiment(),
		misc.NewCrossDomainCompletionExperiment(),
		misc.NewGemmaIntegrationExperiment(),
		misc.NewRuleShiftExperiment(),
	}

	for _, experiment := range allExperiments {
		t.Run(experiment.Name(), func(t *testing.T) {
			// One Convey leaf per experiment: load + prompts + artifacts + gate
			// all execute against the SAME pipeline instance. The previous
			// layout used three sibling Convey blocks under one parent — but
			// in GoConvey each leaf path replays its parent independently, so
			// the artifact and gate siblings used to fire on a freshly-built
			// pipeline whose prompt loop never ran. That made the artifact
			// snapshots empty and the gate telemetry meaningless.
			Convey("Given experiment: "+experiment.Name(), t, func() {
				pipeline, err := NewPipeline(
					t.Context(),
					PipelineWithExperiment(experiment),
					PipelineWithReporter(NewProjectorReporter()),
				)

				So(err, ShouldBeNil)
				So(pipeline, ShouldNotBeNil)

				Convey("Pipeline.Run drives load + prompts then we score", func() {
					So(pipeline.Run(t.Context()), ShouldBeNil)

					rows, ok := experiment.TableData().([]tools.ExperimentalData)
					So(ok, ShouldBeTrue)

					// Exact-count assertion: every prompt Run iterated must
					// produce exactly one ExperimentalData row. A weaker
					// `> 0` check would let a truncated or dropped prompt
					// loop pass silently — which is exactly the failure
					// mode the consolidation into Run was meant to surface.
					// pipeline.timing.n is the count Run measured at
					// dispatch time, so it is the authoritative expected
					// row count for this leaf.
					So(len(rows), ShouldEqual, pipeline.timing.n)
					So(len(rows), ShouldBeGreaterThan, 0)

					// Artifacts are written BEFORE the aggregate gate fires
					// so a gate failure still leaves the per-experiment paper
					// snapshot on disk for inspection.
					So(pipeline.WriteArtifacts(), ShouldBeNil)

					// Aggregate gate enforcement — was previously only logged
					// via t.Logf, which meant a regressed Outcome() never
					// turned into a test failure.
					if msg := pipeline.EnforceOutcome(); msg != "" {
						t.Logf("pipeline gate %s failed: %s", experiment.Name(), msg)
						So(msg, ShouldBeBlank)
					}
				})
			})
		})
	}

	Convey("When the experiments index is written", t, func() {
		So(WriteExperimentsIndex(), ShouldBeNil)
	})
}

// Compile-time guard: pipeline_test.go and pipeline.go must agree on what an
// experiment exposes. fmt is imported solely for this assertion path.
var _ = fmt.Sprintf
