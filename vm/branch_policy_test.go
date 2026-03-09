package vm

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/vm/cortex"
)

func TestCortexGraphConstruction(t *testing.T) {
	Convey("Given a new cortex graph", t, func() {
		graph := cortex.NewGraph()

		Convey("It should have source and sink", func() {
			So(graph.Source(), ShouldNotBeNil)
			So(graph.Sink(), ShouldNotBeNil)
			So(graph.Source(), ShouldNotEqual, graph.Sink())
		})

		Convey("It should start at tick 0", func() {
			So(graph.TickCount(), ShouldEqual, 0)
		})

		Convey("It should have the configured number of nodes", func() {
			So(len(graph.Nodes()), ShouldBeGreaterThanOrEqualTo, 2)
		})
	})
}
