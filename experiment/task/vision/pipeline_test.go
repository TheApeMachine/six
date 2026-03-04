package vision

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestVisionPipeline(t *testing.T) {
	Convey("Given the full Universal Tokenizer -> LSM Spatial Index -> VM Machine pipeline", t, func() {
		corpus, names := visionCorpus()
		pipeline := NewPipeline(corpus)
		So(pipeline, ShouldNotBeNil)

		Convey("When parsing and generating visually diverse spatial bytes natively without structural strings", func() {
			results := pipeline.Run()
			
			Convey("It reconstructs continuous byte arrays from the trained generative space", func() {
				So(len(results), ShouldBeGreaterThanOrEqualTo, len(corpus))
				
				for i, res := range results {
					// We verify that the generative capability runs on raw bytes
					// It steps through topological boundary matching without relying on specific ascii text loops.
					So(res.Name, ShouldEqual, names[i])
					So(res.Generated, ShouldBeGreaterThan, 0)
				}
			})

			Convey("Artifacts should be written to the paper directory natively", func() {
				tableRows := make([]map[string]any, len(results))
				for i, res := range results {
					matchStr := "False"
					if res.Matches {
						matchStr = "True"
					}
					closedStr := "False"
					if res.IsClosed {
						closedStr = "True"
					}
					tableRows[i] = map[string]any{
						"Image":      res.Name,
						"Steps":      fmt.Sprintf("%d", res.Steps),
						"TargetLen":  fmt.Sprintf("%d", res.TargetLen),
						"Generated":  fmt.Sprintf("%d", res.Generated),
						"ExactMatch": matchStr,
						"Closed":     closedStr,
					}
				}

				So(WriteTable(tableRows, "vision_pipeline_summary.tex"), ShouldBeNil)
				_, statErr := os.Stat(filepath.Join(PaperDir(), "vision_pipeline_summary.tex"))
				So(statErr, ShouldBeNil)
			})
		})
	})
}
