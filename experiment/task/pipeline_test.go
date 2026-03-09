package task

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/require"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/task/classification"
	"github.com/theapemachine/six/experiment/task/codegen"
	cortex_task "github.com/theapemachine/six/experiment/task/cortex"
	"github.com/theapemachine/six/experiment/task/imagegen"
	"github.com/theapemachine/six/experiment/task/logic"
	"github.com/theapemachine/six/experiment/task/misc"
	"github.com/theapemachine/six/experiment/task/phasedial"
	"github.com/theapemachine/six/experiment/task/scaling"
	"github.com/theapemachine/six/experiment/task/textgen"
)

func TestPipeline(t *testing.T) {
	t.Skip("Skipping pipeline tests because the core vm prompt generation is temporarily disabled for kernel porting.")
	allExperiments := []tools.PipelineExperiment{
		codegen.NewLanguagesExperiment(),
		classification.NewTextClassificationExperiment(),
		textgen.NewCompositionalExperiment(),
		textgen.NewProseChainingExperiment(),
		textgen.NewOutOfCorpusExperiment(),
		textgen.NewTextOverlapExperiment(),
		phasedial.NewTorusGeneralizationExperiment(),
		imagegen.NewReconstructionExperiment(),
		logic.NewBabiExperiment(),
		misc.NewProteinStructureExperiment(),
		scaling.NewBestFillScalingExperiment(),
		scaling.NewCompressionExperiment(),
		scaling.NewPipelineThroughputExperiment(),
		scaling.NewSequencerExperiment(),
		cortex_task.NewChannelRoutingExperiment(),
	}

	experiments := allExperiments
	if testing.Short() {
		experiments = []tools.PipelineExperiment{
			scaling.NewSequencerExperiment(),
			scaling.NewCompressionExperiment(),
		}
	}

	for _, experiment := range experiments {
		t.Run(experiment.Name(), func(t *testing.T) {
			Convey("Given experiment: "+experiment.Name(), t, func() {
				pipeline, err := NewPipeline(
					PipelineWithExperiment(experiment),
					PipelineWithReporter(NewProjectorReporter()),
				)

				So(err, ShouldBeNil)
				So(pipeline, ShouldNotBeNil)

				Convey("When: "+experiment.Name()+" produces an outcome", func() {
					So(pipeline.Run(), ShouldBeNil)

					outcome, assertion, expected := experiment.Outcome()
					So(outcome, assertion, expected)

					Convey("It should have produced serialized result and artifact snapshots", func() {
						section := experiment.Section()

						resultsPath := filepath.Join(PaperDir(section), tools.Slugify(experiment.Name())+"_results.json")
						_, resultsErr := os.Stat(resultsPath)
						So(resultsErr, ShouldBeNil)

						for _, artifact := range experiment.Artifacts() {
							path := filepath.Join(PaperDir(section), artifactJSONFileName(artifact.FileName))

							_, statErr := os.Stat(path)
							So(statErr, ShouldBeNil)
						}
					})
				})
			})
		})
	}
}

func TestPipelineWithScoreWeights(t *testing.T) {
	experiment := codegen.NewLanguagesExperiment()
	weights := tools.ScoreWeights{Exact: 0.2, Partial: 0.7, Fuzzy: 0.1}

	pipeline, err := NewPipeline(
		PipelineWithExperiment(experiment),
		PipelineWithScoreWeights(weights),
	)

	require.NoError(t, err)
	require.InDelta(t, weights.Exact, pipeline.scoreWgts.Exact, 1e-12)
	require.InDelta(t, weights.Partial, pipeline.scoreWgts.Partial, 1e-12)
	require.InDelta(t, weights.Fuzzy, pipeline.scoreWgts.Fuzzy, 1e-12)
}
