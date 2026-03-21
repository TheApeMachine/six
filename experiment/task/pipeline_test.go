package task

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/task/classification"
	"github.com/theapemachine/six/experiment/task/codegen"
	"github.com/theapemachine/six/experiment/task/imagegen"
	"github.com/theapemachine/six/experiment/task/logic"
	"github.com/theapemachine/six/experiment/task/misc"
	"github.com/theapemachine/six/experiment/task/phasedial"
	"github.com/theapemachine/six/experiment/task/scaling"
	"github.com/theapemachine/six/experiment/task/textgen"
)

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
		phasedial.NewTorusGeneralizationExperiment(),
		phasedial.NewTorusNavigationExperiment(),
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
