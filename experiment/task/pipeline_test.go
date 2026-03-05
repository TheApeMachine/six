package task

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/experiment/task/codegen"
)

func TestPipeline(t *testing.T) {
	experiments := []PipelineExperiment{
		codegen.NewLanguagesExperiment(),
	}
	for _, experiment := range experiments {
		Convey("Given code generation experiment: "+experiment.Name(), t, func() {
			pipeline, err := NewPipeline(
				PipelineWithExperiment(experiment),
			)

			So(err, ShouldBeNil)
			So(pipeline, ShouldNotBeNil)

			Convey("When:"+experiment.Name()+" produces an outcome", func() {
				So(pipeline.Run(), ShouldBeNil)				
				So(experiment.Outcome())

				Convey("It should produce the needed paper artifacts", func() {
					So(WriteTable(
						experiment.TableData(),
						experiment.Name()+"_summary.tex"),
						ShouldBeNil,
					)

					_, statErr := os.Stat(
						filepath.Join(PaperDir(),
							experiment.Name()+"_summary.tex"),
					)

					So(statErr, ShouldBeNil)
				})
			})
		})
	}
}
