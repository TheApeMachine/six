package task

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func finalizePipelineExperimentIfAny(experiment tools.PipelineExperiment) {
	if f, ok := experiment.(interface {
		Finalize(any) error
	}); ok {
		Convey("When the experiment finalizes", func() {
			So(f.Finalize(nil), ShouldBeNil)
		})

		return
	}

	if f, ok := experiment.(interface {
		Finalize() error
	}); ok {
		Convey("When the experiment finalizes", func() {
			So(f.Finalize(), ShouldBeNil)
		})
	}
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

Expectation failures are normal until baselines are met; panics and I/O
errors are not. A full run takes minutes — use a long -timeout (e.g.
go test -timeout 30m ./experiment/task -run TestPipeline).

Each prompt records tools.ExperimentalData via AddResult so Score()/Outcome()
see the same readout path as paper pipelines; prompts without a holdout skip
strict equality only. Experiments with Finalize run it after all prompts.
*/
func TestPipeline(t *testing.T) {
	allExperiments := []tools.PipelineExperiment{
		codegen.NewLanguagesExperiment(),
		classification.NewTextClassificationExperiment(),
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
		scaling.NewBestFillScalingExperiment(),
		scaling.NewCompressionExperiment(),
		scaling.NewPipelineThroughputExperiment(),
		scaling.NewSequencerExperiment(),
	}

	for _, experiment := range allExperiments {
		t.Run(experiment.Name(), func(t *testing.T) {
			Convey("Given experiment: "+experiment.Name(), t, func() {
				pipeline, err := NewPipeline(
					t.Context(),
					PipelineWithExperiment(experiment),
					PipelineWithReporter(NewProjectorReporter()),
					PipelineWithSnapshotReporter(),
					PipelineWithViz(":6600"),
				)

				So(err, ShouldBeNil)
				So(pipeline, ShouldNotBeNil)

				Convey("And a machine", func() {
					machine, err := vm.NewMachine(t.Context(), vm.MachineWithMesh(5))
					So(err, ShouldBeNil)
					So(machine, ShouldNotBeNil)
					So(machine.Load(experiment.Dataset()), ShouldBeNil)

					for idx, prompt := range experiment.Prompts() {
						Convey(fmt.Sprintf("When prompted with [%d] %q", idx, prompt), func() {
							holdoutBytes, holdoutOK := pipeline.experiment.HoldoutForPrompt(idx)
							prediction, err := machine.Prompt(prompt)
							So(err, ShouldBeNil)
							So(prediction, ShouldNotBeNil)

							// Score() / Outcome() read tableData filled by AddResult; without this,
							// aggregate gates see an empty run even when per-prompt checks pass.
							pipeline.experiment.AddResult(tools.ExperimentalData{
								Idx:            idx,
								Name:           fmt.Sprintf("prompt_%d", idx),
								Prefix:         []byte(prompt),
								Holdout:        holdoutBytes,
								Generation:     []byte(prediction.String()),
								Classification: []byte(prediction.Label()),
								Prediction:     prediction,
							})

							if !holdoutOK {
								return
							}

							Convey(
								fmt.Sprintf("It should match holdout for [%d] %s", idx, prompt),
								func() {
									errnie.Debug(
										"experiment",
										"prompt", prompt,
										"holdout", string(holdoutBytes),
										"generation", prediction.String(),
										"classification", prediction.Label(),
									)

									if strings.TrimSpace(prediction.String()) != "" {
										So(
											prediction.String(),
											ShouldEqual,
											string(holdoutBytes),
										)
									}

									if len(prediction.Labels) > 0 {
										So(len(prediction.Labels), ShouldBeGreaterThan, 0)

										for _, score := range prediction.Labels {
											So(score, ShouldBeGreaterThan, 0.0)
										}
									}
								},
							)
						})
					}

					finalizePipelineExperimentIfAny(experiment)
				})

				Convey("It should have the minimum expected outcome for "+experiment.Name(), func() {
					actual, assertion, threshold := experiment.Outcome()
					So(actual, assertion, threshold)
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
