package misc

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	tools "github.com/theapemachine/six/experiment"
)

func TestGemmaIntegrationExperimentFinalize(t *testing.T) {
	t.Parallel()

	Convey("Given substrate rows and stale Gemma comparison rows", t, func() {
		experiment := NewGemmaIntegrationExperiment()
		experiment.tableData = append(experiment.tableData, tools.ExperimentalData{
			Holdout:    []byte("carol"),
			Generation: []byte("carol"),
		})
		experiment.evaluator.Enrich(&experiment.tableData[0])
		experiment.graftResults = append(experiment.graftResults, giResult{Name: "stale"})
		experiment.kvResults = append(experiment.kvResults, giResult{Name: "stale"})

		Convey("When Finalize runs without the external bridge", func() {
			err := experiment.Finalize()

			Convey("It should preserve substrate scoring", func() {
				So(err, ShouldBeNil)
				So(experiment.graftResults, ShouldHaveLength, 0)
				So(experiment.kvResults, ShouldHaveLength, 0)
				So(experiment.Score(), ShouldEqual, 1)
			})
		})
	})
}

func BenchmarkGemmaIntegrationExperimentFinalize(b *testing.B) {
	experiment := NewGemmaIntegrationExperiment()
	experiment.tableData = append(experiment.tableData, tools.ExperimentalData{
		Holdout:    []byte("carol"),
		Generation: []byte("carol"),
	})
	experiment.evaluator.Enrich(&experiment.tableData[0])

	b.ReportAllocs()

	for b.Loop() {
		if err := experiment.Finalize(); err != nil {
			b.Fatal(err)
		}
	}
}
