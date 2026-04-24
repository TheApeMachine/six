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
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/vm"
)

var reporter = NewProjectorReporter()

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
		// scaling.NewSubstrateQueryScalingExperiment(),
		// scaling.NewCompressionExperiment(),
		// scaling.NewPipelineThroughputExperiment(),
		// scaling.NewSequencerExperiment(),
		// codegen.NewLanguagesExperiment(t.Context()),
		classification.NewTextClassificationExperiment(),
		// classification.NewBlindClassificationExperiment(),
		// textgen.NewCompositionalExperiment(),
		// textgen.NewProseChainingExperiment(),
		// textgen.NewOutOfCorpusExperiment(),
		// textgen.NewTextOverlapExperiment(),
		// phasedial.NewAdaptiveSplitExperiment(),
		// phasedial.NewChunkingBaselineExperiment(),
		// phasedial.NewConstraintResolutionExperiment(),
		// phasedial.NewCorrelationLengthExperiment(),
		// phasedial.NewGroupActionEquivarianceExperiment(),
		// phasedial.NewPartialDeletionExperiment(),
		// phasedial.NewPermutationInvarianceExperiment(),
		// phasedial.NewPhaseCoherenceExperiment(),
		// phasedial.NewQueryRobustnessExperiment(),
		// phasedial.NewSnapToSurfaceExperiment(),
		// phasedial.NewSteerabilityExperiment(),
		// phasedial.NewTwoHopRetrievalExperiment(),
		// imagegen.NewReconstructionExperiment(),
		// logic.NewBabiExperiment(),
		// logic.NewSemanticAlgebraExperiment(),
		// misc.NewCrossDomainCompletionExperiment(),
		// misc.NewGemmaIntegrationExperiment(),
		// misc.NewRuleShiftExperiment(),
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
			Convey("Given a machine for experiment: "+experiment.Name(), t, func() {
				machine, err := vm.NewMachine(t.Context())

				So(err, ShouldBeNil)
				So(machine, ShouldNotBeNil)

				Convey("When the dataset is loaded", func() {
					So(machine.Load(experiment.Dataset()), ShouldBeNil)

					Convey("And a prompt is provided", func() {
						for idx, prompt := range experiment.Prompts() {
							values := make([]*primitive.Value, 0)
							label := experiment.LabelForPrompt(idx)

							if label == nil {
								values, err := primitive.NewValue([]byte(prompt))
								So(err, ShouldBeNil)
								So(len(values), ShouldBeGreaterThan, 0)
							} else {
								// Convert the label from string to uint64 (bits)
								labelBits := uint64(0)

								for i, char := range label {
									labelBits |= uint64(char) << (i * 8)
								}

								values, err = primitive.NewValue([]byte(prompt), labelBits)
								So(err, ShouldBeNil)
								So(len(values), ShouldBeGreaterThan, 0)
							}

							resolved, err := machine.Prompt(values...)
							So(err, ShouldBeNil)
							So(len(resolved), ShouldBeGreaterThan, 0)
						}

						Convey("Then the results are scored", func() {
							So(experiment.Outcome())
						})
					})
				})
			})
		})
	}
}

/*
WriteArtifacts emits the paper-side outputs for the experiment under a single
call so test harnesses do not have to interleave summary, results, and per-
artifact writes themselves. Order matters: the standard summary table reads
the same row set the artifact emitters do, so it is written first to keep
"Wall-clock seconds" in the summary consistent with the row count quoted by
the artifact JSON snapshots that follow.
*/
func WriteArtifacts(experiment tools.PipelineExperiment) error {
	if experiment == nil {
		return fmt.Errorf("WriteArtifacts: missing experiment")
	}

	if err := writeStandardSummary(experiment); err != nil {
		return fmt.Errorf("WriteArtifacts: standard summary: %w", err)
	}

	if err := reporter.WriteResults(experiment); err != nil {
		return fmt.Errorf("WriteArtifacts: results snapshot: %w", err)
	}

	for _, artifact := range experiment.Artifacts() {
		if err := reporter.WriteArtifact(experiment, artifact); err != nil {
			return fmt.Errorf(
				"WriteArtifacts: artifact %s (%s): %w",
				artifact.FileName, artifact.Type, err,
			)
		}
	}

	return nil
}

func writeStandardSummary(experiment tools.PipelineExperiment) error {
	rows, ok := experiment.TableData().([]tools.ExperimentalData)

	if !ok || len(rows) == 0 {
		return nil
	}

	holdoutDesc := ""
	if d, ok := experiment.(tools.SummaryHoldoutDescriptor); ok {
		holdoutDesc = d.SummaryHoldoutDescription()
	}

	return WriteStandardSummary(
		experiment.Name(),
		experiment.Section(),
		rows,
		holdoutDesc,
		runTiming{},
	)
}
