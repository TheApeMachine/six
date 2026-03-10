package cortex

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
)

func TestArrive_SignalEchoesAreDeduplicated(t *testing.T) {
	Convey("Given a node receiving the same signal twice", t, func() {
		node := NewNode(0, 0)
		signal := NewSignalToken(data.BaseChord('A'), data.BaseChord('A'), -1)

		Convey("When the signal arrives repeatedly", func() {
			node.Arrive(signal)
			node.Arrive(signal)

			Convey("The node should retain one canonical observation", func() {
				So(len(node.Signals), ShouldEqual, 1)
			})
		})
	})
}

func TestExtractResults_RanksSupportedSignalsFirst(t *testing.T) {
	Convey("Given a sink with multiple routed deductions", t, func() {
		graph := NewGraph()
		garden := NewSignalToken(data.BaseChord('g'), data.BaseChord('g'), -1)
		kitchen := NewSignalToken(data.BaseChord('k'), data.BaseChord('k'), -1)

		graph.sink.rememberSignal(garden)
		graph.sink.rememberSignal(garden)
		graph.sink.rememberSignal(kitchen)

		Convey("When extractResults condenses the sink state", func() {
			results := graph.extractResults()

			Convey("The more strongly supported chord should rank first", func() {
				So(len(results), ShouldBeGreaterThanOrEqualTo, 2)
				So(results[0], ShouldResemble, garden.Chord)
			})
		})
	})
}
