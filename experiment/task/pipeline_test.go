package task

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/task/codegen"
	"github.com/theapemachine/six/experiment/task/logic"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
)

func TestMain(m *testing.M) {
	viper.SetConfigFile("../../cmd/cfg/config.yml")
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "failed loading core value config: %v\n", err)
		os.Exit(1)
	}
	if err := core.LoadValueConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "failed loading value config: %v\n", err)
		os.Exit(1)
	}
	viper.Set("loglevel", "error")
	viper.Set("logging.trace.path", os.DevNull)
	loggingCfg, err := core.LoadLoggingConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed loading logging config: %v\n", err)
		os.Exit(1)
	}
	errnie.InitLogger(loggingCfg)
	code := m.Run()
	os.Exit(code)
}

// TestPipeline runs each experiment through the real pipeline (hydration, prompts,
// scoring, JSON artifacts). Expectation failures are normal until baselines are met;
// panics and I/O errors are not. A full run takes minutes — use a long -timeout
// (e.g. go test -timeout 30m ./experiment/task -run TestPipeline); IDE defaults
// like 30s will kill the suite mid-run.
func TestPipeline(t *testing.T) {
	allExperiments := []tools.PipelineExperiment{
		codegen.NewLanguagesExperiment(),
		// classification.NewTextClassificationExperiment(),
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
		logic.NewBabiExperiment(),
		// logic.NewSemanticAlgebraExperiment(),
		// misc.NewCrossDomainCompletionExperiment(),
		// misc.NewGemmaIntegrationExperiment(),
		// misc.NewRuleShiftExperiment(),
		// scaling.NewBestFillScalingExperiment(),
		// scaling.NewCompressionExperiment(),
		// scaling.NewPipelineThroughputExperiment(),
		// scaling.NewSequencerExperiment(),
	}

	for _, experiment := range allExperiments {
		t.Run(experiment.Name(), func(t *testing.T) {
			Convey("Given experiment: "+experiment.Name(), t, func() {
				// JSON snapshots only — no chromedp/LaTeX/PDF. ProjectorReporter is for
				// paper builds; it can exceed IDE test timeouts when Run awaits Chrome.
				pipeline, err := NewPipeline(
					t.Context(),
					PipelineWithExperiment(experiment),
					PipelineWithReporter(NewSnapshotReporter()),
				)

				So(err, ShouldBeNil)
				So(pipeline, ShouldNotBeNil)

				Convey("When: "+experiment.Name()+" produces an outcome", func() {
					So(pipeline.Run(), ShouldBeNil)

					Convey("It should have the minimum expected outcome for "+experiment.Name(), func() {
						So(experiment.Outcome())
					})

					Convey("It should have produced paper ready artifacts for "+experiment.Name(), func() {
						section := experiment.Section()

						_, resultsErr := os.Stat(
							filepath.Join(
								PaperDir(section),
								tools.Slugify(experiment.Name())+"_results.json",
							),
						)

						So(resultsErr, ShouldBeNil)

						for _, artifact := range experiment.Artifacts() {
							path := filepath.Join(
								PaperDir(section),
								artifactJSONFileName(artifact.FileName),
							)

							_, statErr := os.Stat(path)
							So(statErr, ShouldBeNil)
						}
					})
				})
			})
		})
	}
}
