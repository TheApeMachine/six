package cortex

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

func TestEdgeRefresh_TracksRotationExposedFreeChord(t *testing.T) {
	Convey("Given a connected edge with a latent free chord on face 255", t, func() {
		a := NewNode(0, 0)
		b := NewNode(1, 0)
		a.Connect(b)
		edge := a.Edges()[0]

		fallback := data.BaseChord('B')
		exposed := data.BaseChord('A')
		a.Payload = fallback
		a.Cube.Set(edge.PatchA.Side, edge.PatchA.Rot, 255, exposed)
		a.InvalidateChordCache()

		Convey("At identity rotation the payload fallback should be exposed", func() {
			a.Rot = geometry.IdentityRotation()
			edge.Refresh()
			So(edge.FreeA, ShouldResemble, fallback)
		})

		Convey("After rotating X90 the face aligned to 256 should become exposed", func() {
			a.Rot = geometry.DefaultRotTable.X90
			edge.Refresh()
			So(edge.FreeA, ShouldResemble, exposed)
		})
	})
}

func TestForkPromotion_PromotesRegisterToTool(t *testing.T) {
	Convey("Given a repeated fork motif", t, func() {
		graph := NewGraph()
		parent := graph.Source()
		residue := data.BaseChord('Q')
		program := data.BaseChord('P')

		parent.noteFork(residue, program)
		graph.expandTopology()

		Convey("The first fork should materialize a volatile register", func() {
			registers := graph.RegisterNodes()
			So(len(registers), ShouldEqual, 1)
			So(registers[0].Role, ShouldEqual, RoleRegister)
		})

		register := graph.RegisterNodes()[0]
		register.noteFork(residue, program)
		graph.expandTopology()
		graph.promoteTools()

		Convey("Repeating the same fork should promote the register into a tool", func() {
			tools := graph.ToolNodes()
			So(len(tools), ShouldEqual, 1)
			So(tools[0].Role, ShouldEqual, RoleTool)
			So(tools[0].Program, ShouldResemble, program)
		})
	})
}

func TestResetPromptCycle_PreservesCompiledTools(t *testing.T) {
	Convey("Given a graph with a promoted tool and a volatile register", t, func() {
		graph := NewGraph()
		parent := graph.Source()
		residue := data.BaseChord('Q')
		program := data.BaseChord('P')

		parent.noteFork(residue, program)
		graph.expandTopology()
		register := graph.RegisterNodes()[0]
		register.noteFork(residue, program)
		graph.expandTopology()
		graph.promoteTools()
		graph.SpawnNode(parent)

		Convey("When the prompt cycle resets", func() {
			graph.ResetPromptCycle()

			Convey("Tool nodes should survive while registers are discarded", func() {
				So(len(graph.ToolNodes()), ShouldEqual, 1)
				So(len(graph.RegisterNodes()), ShouldEqual, 0)
				So(len(graph.nodes), ShouldEqual, graph.initialNodes+1)
			})

			Convey("The surviving tool should still be attached to the compute fabric", func() {
				tool := graph.ToolNodes()[0]
				So(tool.EdgeCount(), ShouldBeGreaterThanOrEqualTo, 1)
				So(tool.Program, ShouldResemble, program)
			})
		})
	})
}

func TestToolInvocation_EmitsReusableSignal(t *testing.T) {
	Convey("Given a compiled tool node", t, func() {
		graph := NewGraph()
		input := data.BaseChord('Q')
		output := data.BaseChord('A')
		program := data.BaseChord('P')

		tool := graph.spawnSpecializedNode(graph.Source(), RoleTool, input, output, program)
		graph.toolCatalog[tool.candidateToolKey()] = tool
		tool.Connect(graph.Sink())
		graph.Sink().Connect(tool)

		query := NewSignalToken(input, input, graph.Source().ID)
		tool.Arrive(query)
		drained := graph.Sink().DrainInbox()

		Convey("Matching the interface should emit the tool's payload as a signal", func() {
			found := false
			for _, tok := range drained {
				if tok.IsSignal && data.ChordSimilarity(&tok.Chord, &output) == output.ActiveCount() {
					found = true
					break
				}
			}
			So(found, ShouldBeTrue)
		})
	})
}

func TestSnapshotLogic_ExportsToolAndRegisterRules(t *testing.T) {
	Convey("Given a graph with one compiled tool and one volatile register", t, func() {
		graph := NewGraph()
		tool := graph.spawnSpecializedNode(graph.Source(), RoleTool, data.BaseChord('I'), data.BaseChord('O'), data.BaseChord('P'))
		tool.Support = 5
		graph.toolCatalog[tool.candidateToolKey()] = tool

		register := graph.SpawnRegister(graph.Source(), data.BaseChord('R'), data.BaseChord('S'), data.BaseChord('T'))
		register.Support = 2

		Convey("SnapshotLogic should expose both durable and prompt-local rules", func() {
			snapshot := graph.SnapshotLogic()
			So(snapshot.Empty(), ShouldBeFalse)
			So(len(snapshot.Rules), ShouldBeGreaterThanOrEqualTo, 2)

			foundTool := false
			foundRegister := false
			for _, rule := range snapshot.Rules {
				if rule.Role == RoleTool && rule.Interface == data.BaseChord('I') && rule.Payload == data.BaseChord('O') {
					foundTool = true
				}
				if rule.Role == RoleRegister && rule.Interface == data.BaseChord('R') && rule.Payload == data.BaseChord('S') {
					foundRegister = true
				}
			}

			So(foundTool, ShouldBeTrue)
			So(foundRegister, ShouldBeTrue)
		})
	})
}

func TestSnapshotLogic_ExportsComposedToolChains(t *testing.T) {
	Convey("Given two tool nodes linked by a stable compose edge", t, func() {
		graph := NewGraph()
		left := graph.spawnSpecializedNode(graph.Source(), RoleTool, data.BaseChord('A'), data.BaseChord('C'), data.BaseChord('P'))
		right := graph.spawnSpecializedNode(graph.Source(), RoleTool, data.BaseChord('C'), data.BaseChord('D'), data.BaseChord('Q'))
		left.Support = 6
		right.Support = 5
		graph.toolCatalog[left.candidateToolKey()] = left
		graph.toolCatalog[right.candidateToolKey()] = right

		left.Connect(right)
		right.Connect(left)
		edge := left.Edges()[len(left.Edges())-1]
		edge.Op = OpCompose
		edge.ComposeHits = 4
		edge.Program = data.ChordOR(&left.Program, &right.Program)

		Convey("SnapshotLogic should export the two-step chain", func() {
			snapshot := graph.SnapshotLogic()
			So(len(snapshot.Chains), ShouldBeGreaterThanOrEqualTo, 1)

			found := false
			for _, chain := range snapshot.Chains {
				if chain.Left.Interface == data.BaseChord('A') && chain.Right.Payload == data.BaseChord('D') {
					found = true
					break
				}
			}

			So(found, ShouldBeTrue)
		})
	})
}

func TestSnapshotLogic_ExportsLongRangeToolCircuits(t *testing.T) {
	Convey("Given three tool nodes linked by stable compose edges", t, func() {
		graph := NewGraph()
		first := graph.spawnSpecializedNode(graph.Source(), RoleTool, data.BaseChord('A'), data.BaseChord('C'), data.BaseChord('P'))
		second := graph.spawnSpecializedNode(graph.Source(), RoleTool, data.BaseChord('C'), data.BaseChord('D'), data.BaseChord('Q'))
		third := graph.spawnSpecializedNode(graph.Source(), RoleTool, data.BaseChord('D'), data.BaseChord('E'), data.BaseChord('R'))
		first.Support = 7
		second.Support = 6
		third.Support = 5
		graph.toolCatalog[first.candidateToolKey()] = first
		graph.toolCatalog[second.candidateToolKey()] = second
		graph.toolCatalog[third.candidateToolKey()] = third

		first.Connect(second)
		second.Connect(first)
		edgeAB := first.Edges()[len(first.Edges())-1]
		edgeAB.Op = OpCompose
		edgeAB.ComposeHits = 5
		edgeAB.Program = data.ChordOR(&first.Program, &second.Program)

		second.Connect(third)
		third.Connect(second)
		edgeBC := second.Edges()[len(second.Edges())-1]
		edgeBC.Op = OpCompose
		edgeBC.ComposeHits = 4
		edgeBC.Program = data.ChordOR(&second.Program, &third.Program)

		graph.compileCircuits()

		Convey("SnapshotLogic should export a circuit with three sequential steps", func() {
			snapshot := graph.SnapshotLogic()
			So(len(snapshot.Circuits), ShouldBeGreaterThanOrEqualTo, 1)

			found := false
			for _, circuit := range snapshot.Circuits {
				if circuit.Len() < 3 {
					continue
				}
				payloads := circuit.Payloads()
				if len(payloads) >= 3 && payloads[0] == data.BaseChord('C') && payloads[1] == data.BaseChord('D') && payloads[2] == data.BaseChord('E') {
					found = true
					break
				}
			}

			So(found, ShouldBeTrue)
		})
	})
}

func BenchmarkSnapshotLogic(b *testing.B) {
	graph := NewGraph()
	first := graph.spawnSpecializedNode(graph.Source(), RoleTool, data.BaseChord('A'), data.BaseChord('C'), data.BaseChord('P'))
	second := graph.spawnSpecializedNode(graph.Source(), RoleTool, data.BaseChord('C'), data.BaseChord('D'), data.BaseChord('Q'))
	third := graph.spawnSpecializedNode(graph.Source(), RoleTool, data.BaseChord('D'), data.BaseChord('E'), data.BaseChord('R'))
	first.Support = 7
	second.Support = 6
	third.Support = 5
	graph.toolCatalog[first.candidateToolKey()] = first
	graph.toolCatalog[second.candidateToolKey()] = second
	graph.toolCatalog[third.candidateToolKey()] = third

	first.Connect(second)
	second.Connect(first)
	edgeAB := first.Edges()[len(first.Edges())-1]
	edgeAB.Op = OpCompose
	edgeAB.ComposeHits = 5
	edgeAB.Program = data.ChordOR(&first.Program, &second.Program)

	second.Connect(third)
	third.Connect(second)
	edgeBC := second.Edges()[len(second.Edges())-1]
	edgeBC.Op = OpCompose
	edgeBC.ComposeHits = 4
	edgeBC.Program = data.ChordOR(&second.Program, &third.Program)

	graph.compileCircuits()

	for i := 0; i < b.N; i++ {
		_ = graph.SnapshotLogic()
	}
}
