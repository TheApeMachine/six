package cortex

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
)

func TestResetPromptCycle_ClearsVolatileReasoningState(t *testing.T) {
	Convey("Given a graph polluted by a previous prompt", t, func() {
		graph := NewGraph()
		graph.SpawnNode(graph.Source())
		graph.sink.Cube.Set(0, 0, 42, data.BaseChord('Z'))
		graph.sink.InvalidateChordCache()
		graph.sink.Arrive(NewSignalToken(data.BaseChord('Q'), data.BaseChord('Q'), -1))

		Convey("When ResetPromptCycle is called", func() {
			graph.ResetPromptCycle()

			Convey("It should restore the base node count and clear sink state", func() {
				So(len(graph.nodes), ShouldEqual, graph.initialNodes)
				So(graph.sink.Energy(), ShouldEqual, 0)
				So(len(graph.sink.Signals), ShouldEqual, 0)
			})

			Convey("It should rebuild a connected scratchpad topology", func() {
				for _, node := range graph.nodes {
					So(node.EdgeCount(), ShouldBeGreaterThanOrEqualTo, 2)
				}
			})
		})
	})
}

func TestSpawnNode_SeedsParentResidue(t *testing.T) {
	Convey("Given a parent node with an unresolved residue", t, func() {
		graph := NewGraph()
		parent := graph.Source()

		c20 := data.BaseChord(20)
		c21 := data.BaseChord(21)
		peak := data.ChordOR(&c20, &c21)
		side := data.BaseChord(10)

		parent.Cube.Set(0, 0, 20, peak)
		parent.Cube.Set(0, 0, 10, side)
		parent.InvalidateChordCache()

		_, hole, _, shouldDream := parent.Hole()

		Convey("The parent should expose a non-empty search residue", func() {
			So(shouldDream, ShouldBeTrue)
			So(hole.ActiveCount(), ShouldBeGreaterThan, 0)
		})

		Convey("When SpawnNode is called", func() {
			child := graph.SpawnNode(parent)
			childSummary := child.CubeChord()

			Convey("The child should inherit the unresolved residue instead of starting empty", func() {
				So(childSummary.ActiveCount(), ShouldBeGreaterThan, 0)
				So(data.ChordSimilarity(&childSummary, &hole), ShouldEqual, hole.ActiveCount())
			})

			Convey("The child should inherit the parent's rotational frame", func() {
				So(child.Rot, ShouldResemble, parent.Rot)
			})
		})
	})
}
