package cortex

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/experiment/task"
)

func TestChannelRoutingPipeline(t *testing.T) {
	Convey("Given the ChannelRoutingExperiment running through the Pipeline", t, func() {
		exp := NewChannelRoutingExperiment()
		pipeline, err := task.NewPipeline(
			task.PipelineWithExperiment(exp),
			task.PipelineWithReporter(task.NewProjectorReporter()),
		)

		So(err, ShouldBeNil)
		So(pipeline, ShouldNotBeNil)

		Convey("When the pipeline runs", func() {
			err := pipeline.Run()
			So(err, ShouldBeNil)

			Convey("It should generate the required artifacts and pass the assertion", func() {
				outcome, assertion, expected := exp.Outcome()
				So(outcome, assertion, expected)
			})
		})
	})
}
