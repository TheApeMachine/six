package codegen

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestPipeline(t *testing.T) {
	Convey("Given the full HuggingFace→Tokenizer→LSM pipeline", t, func() {
		pipeline := NewPipeline()
		So(pipeline, ShouldNotBeNil)

		Convey("When running the pipeline", func() {
			pipeline.Run()
			Convey("Pipeline completes without panicking", func() {
				So(pipeline, ShouldNotBeNil)
			})
		})
	})
}
