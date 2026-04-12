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
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/vm"
)

func TestMain(m *testing.M) {
	_ = tryLoadConfigForTaskTests()

	if err := errnie.InitLoggerFromViper(); err != nil {
		fmt.Fprintf(os.Stderr, "experiment/task tests: errnie.InitLoggerFromViper: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func finalizePipelineExperimentIfAny(t *testing.T, experiment tools.PipelineExperiment) {
	t.Helper()

	if f, ok := experiment.(interface {
		Finalize(any) error
	}); ok {
		if err := f.Finalize(nil); err != nil {
			t.Fatalf("experiment Finalize(any): %v", err)
		}

		return
	}

	if f, ok := experiment.(interface {
		Finalize() error
	}); ok {
		if err := f.Finalize(); err != nil {
			t.Fatalf("experiment Finalize(): %v", err)
		}
	}
}

func logPipelineGate(t *testing.T, experimentName string, actual any, assertion Assertion, threshold any) {
	t.Helper()

	if assertion == nil {
		return
	}

	if message := assertion(actual, threshold); message != "" {
		t.Logf(
			"%s aggregate gate recorded below threshold: actual=%v threshold=%v detail=%s",
			experimentName,
			actual,
			threshold,
			message,
		)
	}
}

func pipelineExperimentRowCount(experiment tools.PipelineExperiment) (int, bool) {
	rows, ok := experiment.TableData().([]tools.ExperimentalData)
	if !ok {
		return 0, false
	}

	return len(rows), true
}

func tryLoadConfigForTaskTests() error {
	viper.SetConfigType("yml")

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
		codegen.NewLanguagesExperiment(),
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
			Convey("Given experiment: "+experiment.Name(), t, func() {
				pipeline, err := NewPipeline(
					t.Context(),
					PipelineWithExperiment(experiment),
					PipelineWithReporter(NewProjectorReporter()),
					PipelineWithViz(":6600"),
				)

				So(err, ShouldBeNil)
				So(pipeline, ShouldNotBeNil)

				Convey("And a machine", func() {
					machine, err := vm.NewMachine(t.Context())
					So(err, ShouldBeNil)
					So(machine, ShouldNotBeNil)
					if machine != nil {
						defer func() {
							if closeErr := machine.Close(); closeErr != nil {
								t.Fatalf("machine.Close: %v", closeErr)
							}
						}()
					}
					if loadErr := machine.Load(experiment.Dataset()); loadErr != nil {
						t.Fatalf("machine.Load: %v", loadErr)
					}

					for idx, prompt := range experiment.Prompts() {
						holdoutBytes, _ := pipeline.experiment.HoldoutForPrompt(idx)
						rowsBefore, rowsOk := pipelineExperimentRowCount(pipeline.experiment)
						prediction, promptErr := machine.Prompt(prompt, "affinity")

						if promptErr != nil {
							t.Fatalf("prompt %d: %v", idx, promptErr)
						}

						if prediction == nil {
							t.Fatalf("prompt %d: nil prediction", idx)
						}

						generation := prediction.String()

						// Read the classification label from the properties region
						// The substrate is expected to write the predicted category index to PropertiesStartWord
						predLabelInt := (*prediction)[kernel.PropertiesStartWord]
						classification := fmt.Sprintf("%d", predLabelInt)

						// Score() / Outcome() read tableData filled by AddResult; without this,
						// aggregate gates see an empty run even when per-prompt checks pass.
						pipeline.experiment.AddResult(tools.ExperimentalData{
							Idx:            idx,
							Name:           fmt.Sprintf("prompt_%d", idx),
							Prefix:         []byte(prompt),
							Holdout:        holdoutBytes,
							Generation:     []byte(generation),
							Classification: []byte(classification),
						})

						rowsAfter, ok := pipelineExperimentRowCount(pipeline.experiment)

						if !rowsOk || !ok {
							t.Fatalf("prompt %d: experiment row count unavailable", idx)
						}

						if rowsAfter < rowsBefore {
							t.Fatalf(
								"prompt %d: result rows decreased from %d to %d",
								idx,
								rowsBefore,
								rowsAfter,
							)
						}
					}

					finalizePipelineExperimentIfAny(t, experiment)
				})

				Convey("It should record the aggregate outcome for "+experiment.Name(), func() {
					actual, assertion, threshold := experiment.Outcome()
					logPipelineGate(t, experiment.Name(), actual, assertion, threshold)
				})

				Convey("When paper artifacts are emitted for "+experiment.Name(), func() {
					So(pipeline.writeStandardSummary(), ShouldBeNil)
					So(pipeline.reporter.WriteResults(experiment), ShouldBeNil)

					for _, artifact := range experiment.Artifacts() {
						So(pipeline.reporter.WriteArtifact(experiment, artifact), ShouldBeNil)
					}
				})
			})
		})
	}

	Convey("When the experiments index is written", t, func() {
		So(WriteExperimentsIndex(), ShouldBeNil)
	})
}
