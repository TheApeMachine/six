package task

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestPipeline(t *testing.T) {
	for _, experiment := range []PipelineExperiment{} {
		Convey("Given code generation experiment: "+experiment.Name(), t, func() {
			pipeline := NewPipeline(
				PipelineWithExperiment(experiment),
			)

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
