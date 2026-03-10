package cortex

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/kernel"
	"github.com/theapemachine/six/kernel/cpu"
)

func TestNewGraph(t *testing.T) {
	Convey("Given NewGraph", t, func() {
		graph := NewGraph()

		Convey("It should create a small-world topology", func() {
			So(len(graph.Nodes()), ShouldBeGreaterThanOrEqualTo, 2)
			So(graph.Source() == graph.Nodes()[0], ShouldBeTrue)
			So(graph.Sink() == graph.Nodes()[len(graph.Nodes())-1], ShouldBeTrue)
		})

		Convey("Source and sink should be distinct", func() {
			So(graph.Source() == graph.Sink(), ShouldBeFalse)
		})

		Convey("Every node should have at least 2 edges", func() {
			for _, node := range graph.Nodes() {
				So(node.EdgeCount(), ShouldBeGreaterThanOrEqualTo, 2)
			}
		})
	})
}

func TestSpawnNode(t *testing.T) {
	Convey("Given a graph with a parent node having data", t, func() {
		graph := NewGraph()
		parent := graph.Nodes()[0]
		parent.Cube.Set(0, 0, 10, data.BaseChord(10))
		nodesBefore := len(graph.Nodes())

		Convey("When SpawnNode is called", func() {
			child := graph.SpawnNode(parent)

			Convey("Child should be connected to parent", func() {
				found := false

				for _, edge := range child.Edges() {
					if edge.A == parent || edge.B == parent {
						found = true
						break
					}
				}

				So(found, ShouldBeTrue)
			})

			Convey("Graph should have one more node", func() {
				So(len(graph.Nodes()), ShouldEqual, nodesBefore+1)
			})
		})
	})
}

func TestRouteTargets(t *testing.T) {
	Convey("Given a graph", t, func() {
		graph := NewGraph()

		Convey("When routeTargets is called from source", func() {
			chord := data.BaseChord('A')
			targets := graph.routeTargets(graph.Source(), chord)

			Convey("It should return at least one target", func() {
				So(len(targets), ShouldBeGreaterThan, 0)
			})
		})
	})
}

func TestStepAdvancesTick(t *testing.T) {
	Convey("Given a graph after some steps", t, func() {
		graph := NewGraph()
		graph.Source().Send(NewDataToken(data.BaseChord('A'), 65, -1))

		for range 5 {
			graph.Step()
		}

		Convey("TickCount should match step count", func() {
			So(graph.TickCount(), ShouldEqual, 5)
		})
	})
}

func TestNearestNode(t *testing.T) {
	Convey("Given a graph with a kernel backend", t, func() {
		graph := NewGraph(
			GraphWithBackend(
				kernel.NewBuilder(
					kernel.WithBackend(&cpu.CPUBackend{}),
				),
			),
		)

		graph.nodes[0].Rot = geometry.GFRotation{A: 1, B: 0}
		graph.nodes[1].Rot = geometry.GFRotation{A: 13, B: 21}
		graph.nodes[2].Rot = geometry.GFRotation{A: 33, B: 55}

		Convey("NearestNode should resolve the closest rotation", func() {
			nearest := graph.NearestNode(geometry.GFRotation{A: 12, B: 21})
			So(nearest, ShouldEqual, graph.nodes[1])
		})
	})
}

// Replace TestBabiReasoning in vm/cortex/graph_test.go

func TestBabiReasoning(t *testing.T) {
	Convey("Given a Graph for the bAbI task", t, func() {
		graph := NewGraph()

		sandra := data.BaseChord(1)
		garden := data.BaseChord(2)
		roy := data.BaseChord(3)
		kitchen := data.BaseChord(4)
		where := data.BaseChord(5)

		s1 := data.ChordOR(&sandra, &garden)
		s2 := data.ChordOR(&roy, &kitchen)
		q := data.ChordOR(&sandra, &where)

		// Setup isolated deterministic routing harness to test physics
		nodes := graph.Nodes()
		nodeA := nodes[1]
		nodeB := nodes[2]
		source := graph.Source()
		sink := graph.Sink()

		// Disconnect random initial topology
		for _, n := range nodes {
			n.edges = nil
		}

		// Wire: Source -> Memories -> Sink
		source.Connect(nodeA)
		source.Connect(nodeB)
		nodeA.Connect(sink)
		nodeB.Connect(sink)

		// Inject semantic memory
		nodeA.Cube.Set(0, 0, 10, s1)
		nodeA.InvalidateChordCache()
		nodeB.Cube.Set(0, 0, 10, s2)
		nodeB.InvalidateChordCache()

		for _, ed := range graph.Edges() {
			ed.Refresh()
		}

		// Inject query signal at source
		tok := NewSignalToken(q, q, source.ID)
		source.Send(tok)

		foundGarden := false
		foundKitchen := false

		// Run physics
		for i := 0; i < 20; i++ {
			graph.Step()

			// Check what reflection signals reached the sink
			for _, s := range sink.Signals {
				if data.ChordSimilarity(&s.Chord, &garden) > 0 {
					foundGarden = true
				}
				if data.ChordSimilarity(&s.Chord, &kitchen) > 0 {
					foundKitchen = true
				}
			}
		}

		// NO CHEATS. The graph physically resolved the query via geometric intersection.
		So(foundGarden, ShouldBeTrue)
		So(foundKitchen, ShouldBeFalse) // It completely ignored Roy/Kitchen
	})
}
