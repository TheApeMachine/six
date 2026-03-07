package task

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/require"
	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/task/classification"
	"github.com/theapemachine/six/experiment/task/codegen"
	"github.com/theapemachine/six/experiment/task/phasedial"
	"github.com/theapemachine/six/experiment/task/textgen"
)

func labelValue(label *int) any {
	if label == nil {
		return nil
	}

	return *label
}

func TestPipeline(t *testing.T) {
	experiments := []tools.PipelineExperiment{
		codegen.NewLanguagesExperiment(),
		classification.NewTextClassificationExperiment(),
		textgen.NewTextOverlapExperiment(),
		textgen.NewOutOfCorpusExperiment(),
		phasedial.NewTorusGeneralizationExperiment(),
	}

	for _, experiment := range experiments {
		Convey("Given experiment: "+experiment.Name(), t, func() {
			pipeline, err := NewPipeline(
				PipelineWithExperiment(experiment),
			)

			So(err, ShouldBeNil)
			So(pipeline, ShouldNotBeNil)

			Convey("When: "+experiment.Name()+" produces an outcome", func() {
				So(pipeline.Run(), ShouldBeNil)

				outcome, assertion, expected := experiment.Outcome()
				So(outcome, assertion, expected)

				Convey("It should have produced the expected artifacts", func() {
					section := experiment.Section()

					// Every experiment should at least have a summary table (implicitly or explicitly)
					// But we only check the ones defined in Artifacts() or the main summary if we still write it.
					// Since we moved everything to Artifacts(), we check those.

					for _, artifact := range experiment.Artifacts() {
						path := filepath.Join(PaperDir(section), artifact.FileName)
						if !strings.HasSuffix(path, ".tex") && !strings.HasSuffix(path, ".pdf") && !strings.Contains(path, ".") {
							// Artifacts without extension are likely charts (which become .pdf)
							path += ".pdf"
						}

						_, statErr := os.Stat(path)
						So(statErr, ShouldBeNil)
					}
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
