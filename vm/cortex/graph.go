package cortex

import (
	"context"
	"errors"
	"unsafe"

	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/errnie"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/kernel"
	"github.com/theapemachine/six/kernel/cpu"
	"github.com/theapemachine/six/kernel/cuda"
	"github.com/theapemachine/six/kernel/metal"
	"github.com/theapemachine/six/pool"
)

/*
Graph is the cortex compute fabric: a standing core of logic nodes, volatile
forked registers, and reusable tool nodes compiled out of repeated graph
behavior. External communication goes through the pool.
*/
type Graph struct {
	ctx     context.Context
	cancel  context.CancelFunc
	backend kernel.Backend
	nodes   []*Node
	source  *Node
	sink    *Node
	tick    int
	nextID  int

	initialNodes int

	sinkStableCount int
	sinkLastEnergy  float64

	seqPos uint32
	seqZ   uint8

	momentum         float64
	lastRotationTick int

	bedrockQueries int
	mitosisEvents  int
	pruneEvents    int

	outputEmitted    bool
	toolCatalog      map[toolKey]*Node
	toolVotes        map[toolKey]int
	compositeCatalog map[toolPairKey]*Node
	circuitCatalog   map[string]LogicCircuit

	broadcast *pool.BroadcastGroup
}

type graphOpts func(*Graph)

/*
NewGraph creates a cortex graph with the specified configuration.

The initial topology is a small-world network: a ring of N nodes with 2
random long-range edges per node. The source is node 0, the sink is node N-1.
*/
func NewGraph(opts ...graphOpts) *Graph {
	graph := &Graph{
		nodes:            make([]*Node, 0, 8),
		momentum:         0.0,
		toolCatalog:      make(map[toolKey]*Node),
		toolVotes:        make(map[toolKey]int),
		compositeCatalog: make(map[toolPairKey]*Node),
		circuitCatalog:   make(map[string]LogicCircuit),
	}

	for _, opt := range opts {
		opt(graph)
	}

	if graph.backend == nil {
		switch config.System.Backend {
		case "metal":
			graph.backend = kernel.NewBuilder(
				kernel.WithBackend(&metal.MetalBackend{}),
			)
		case "cuda":
			graph.backend = kernel.NewBuilder(
				kernel.WithBackend(&cuda.CUDABackend{}),
			)
		case "cpu":
			graph.backend = kernel.NewBuilder(
				kernel.WithBackend(&cpu.CPUBackend{}),
			)
		}
	}

	for idx := range config.Cortex.InitialNodes {
		node := NewNode(idx, 0)
		graph.nodes = append(graph.nodes, node)
		graph.nextID = idx + 1
	}

	graph.initialNodes = len(graph.nodes)
	graph.rebuildBaseTopology()
	graph.lastRotationTick = graph.tick

	return graph
}

/*
Tick processes a PoolValue and steps the cortex.
*/
func (graph *Graph) Tick(result *pool.Result) {
	if result != nil && result.Value != nil {
		if pv, ok := result.Value.(pool.PoolValue[[]data.Chord]); ok {
			if pv.Key == "prompt" {
				graph.ResetPromptCycle()
				graph.InjectChords(pv.Value)
			}
		}
	}

	graph.Step()
}

/*
Nodes returns the current node list.
*/
func (graph *Graph) Nodes() []*Node { return graph.nodes }

/*
Source returns the prompt injection node.
*/
func (graph *Graph) Source() *Node { return graph.source }

/*
Sink returns the output extraction node.
*/
func (graph *Graph) Sink() *Node { return graph.sink }

/*
TickCount returns the number of Step() calls completed.
*/
func (graph *Graph) TickCount() int { return graph.tick }

/*
SpawnNode creates a volatile register node focused on the parent's strongest
unresolved residue. Tool nodes are promoted separately once repeated fork
patterns stabilize.
*/
func (graph *Graph) SpawnNode(parent *Node) *Node {
	anchor, hole, _, shouldDream := parent.Hole()
	input := hole
	if !shouldDream || input.ActiveCount() == 0 {
		input = parent.SearchChord()
	}

	program := parent.Program
	if program.ActiveCount() == 0 {
		program = anchor
	}

	payload := anchor
	if payload.ActiveCount() == 0 {
		payload = input
	}

	return graph.SpawnRegister(parent, input, payload, program)
}

/*
routeTargets returns the set of nodes a token should be sent to.

Structured medium (at least one neighbor resonates): single best neighbor.
Unstructured medium (all zero): omnidirectional wave propagation.
*/
func (graph *Graph) routeTargets(from *Node, chord data.Chord) []*Node {
	var best *Node
	bestScore := -1.0
	allZero := true

	for _, edge := range from.edges {
		neighbor := edge.A

		if neighbor == from {
			neighbor = edge.B
		}

		neighborSummary := neighbor.CubeChord()
		score := float64(data.ChordSimilarity(&chord, &neighborSummary))

		if score > 0 {
			allZero = false
		}

		if score > bestScore {
			bestScore = score
			best = neighbor
		}
	}

	if allZero {
		var neighbors []*Node

		for _, edge := range from.edges {
			if edge.A == from {
				neighbors = append(neighbors, edge.B)
			} else {
				neighbors = append(neighbors, edge.A)
			}
		}

		return neighbors
	}

	if best != nil {
		return []*Node{best}
	}

	return nil
}

/*
Wipe clears all 257 faces of the node's working memory.
*/
func (node *Node) Wipe() {
	for side := 0; side < 6; side++ {
		for rot := 0; rot < 4; rot++ {
			for face := 0; face < 257; face++ {
				node.Cube.Set(side, rot, face, data.Chord{})
			}
		}
	}

	node.InvalidateChordCache()
}

/*
Wipe clears the working memory of all nodes in the graph.
*/
func (graph *Graph) Wipe() {
	for _, node := range graph.nodes {
		node.Wipe()
	}
}

/*
WipeFace clears a specific logical face across all nodes in the graph.
*/
func (graph *Graph) WipeFace(logicalFace int) {
	for _, node := range graph.nodes {
		node.WipeFace(logicalFace)
	}
}

/*
ResetPromptCycle clears prompt-local graph activity while preserving compiled
tool nodes. Registers are discarded; tools are reseeded and reattached to the
base compute fabric.
*/
func (graph *Graph) ResetPromptCycle() {
	if graph.initialNodes == 0 {
		return
	}

	kept := make([]*Node, 0, len(graph.nodes))

	for idx, node := range graph.nodes {
		keepTool := idx >= graph.initialNodes && node.Role == RoleTool
		if idx < graph.initialNodes || keepTool {
			node.ResetForPrompt(graph.tick, keepTool)
			kept = append(kept, node)
		}
	}

	graph.nodes = kept
	graph.nextID = 0
	for _, node := range graph.nodes {
		if node.ID >= graph.nextID {
			graph.nextID = node.ID + 1
		}
	}

	graph.rebuildBaseTopology()
	graph.toolVotes = make(map[toolKey]int)
	graph.compositeCatalog = make(map[toolPairKey]*Node)
	graph.reindexToolCatalog()

	graph.sinkStableCount = 0
	graph.sinkLastEnergy = 0
	graph.seqPos = 0
	graph.seqZ = 0
	graph.momentum = 0
	graph.lastRotationTick = graph.tick
	graph.bedrockQueries = 0
	graph.mitosisEvents = len(graph.ToolNodes())
	graph.pruneEvents = 0
	graph.outputEmitted = false
}

/*
rebuildBaseTopology restores the standing core ring and then reattaches any
surviving tool nodes as resonant peripherals.
*/
func (graph *Graph) rebuildBaseTopology() {
	nodeCount := len(graph.nodes)
	if nodeCount == 0 {
		graph.source = nil
		graph.sink = nil
		return
	}

	for idx := 0; idx < nodeCount; idx++ {
		graph.nodes[idx].edges = nil
	}

	coreCount := graph.initialNodes
	if coreCount > nodeCount {
		coreCount = nodeCount
	}

	if coreCount == 0 {
		graph.source = nil
		graph.sink = nil
		return
	}

	for idx := 0; idx < coreCount; idx++ {
		graph.nodes[idx].Connect(graph.nodes[(idx+1)%coreCount])
		graph.nodes[(idx+1)%coreCount].Connect(graph.nodes[idx])
	}

	for idx := 0; idx < coreCount; idx++ {
		far1 := (idx + coreCount/3) % coreCount
		far2 := (idx + 2*coreCount/3) % coreCount

		if far1 != idx {
			graph.nodes[idx].Connect(graph.nodes[far1])
			graph.nodes[far1].Connect(graph.nodes[idx])
		}

		if far2 != idx {
			graph.nodes[idx].Connect(graph.nodes[far2])
			graph.nodes[far2].Connect(graph.nodes[idx])
		}
	}

	graph.source = graph.nodes[0]
	graph.sink = graph.nodes[coreCount-1]

	for idx := coreCount; idx < nodeCount; idx++ {
		graph.attachSpecialNode(graph.nodes[idx])
	}
}

/*
NearestNode uses the kernel backend to find the node whose
GF(257) rotational state is closest to the target rotation.
*/
func (graph *Graph) NearestNode(target geometry.GFRotation) *Node {
	if graph.backend == nil || !graph.backend.Available() {
		return nil
	}

	nodeCount := len(graph.nodes)

	if nodeCount == 0 {
		return nil
	}

	layout := make([]geometry.GFRotation, nodeCount)

	for idx, node := range graph.nodes {
		layout[idx] = node.Rot
	}

	return errnie.FlatMap(
		errnie.Try(graph.backend.Resolve(
			unsafe.Pointer(&layout[0]),
			nodeCount,
			unsafe.Pointer(&target),
		)),
		func(packed uint64) (*Node, error) {
			bestIdx, _ := kernel.DecodePacked(packed)

			if bestIdx < 0 || bestIdx >= nodeCount {
				return nil, errors.New("nearest node index out of range")
			}

			return graph.nodes[bestIdx], nil
		},
	).Value()
}

/*
GraphWithContext adds a context to the graph.
*/
func GraphWithContext(ctx context.Context) graphOpts {
	return func(graph *Graph) {
		graph.ctx, graph.cancel = context.WithCancel(ctx)
	}
}

/*
GraphWithBackend injects the GPU kernel backend.
*/
func GraphWithBackend(backend kernel.Backend) graphOpts {
	return func(graph *Graph) {
		graph.backend = backend
	}
}

/*
GraphWithBroadcast sets the broadcast group for outputting results.
*/
func GraphWithBroadcast(broadcast *pool.BroadcastGroup) graphOpts {
	return func(graph *Graph) {
		graph.broadcast = broadcast
	}
}

type GraphError string

const (
	ErrBadValue GraphError = "bad value"
)

func (err GraphError) Error() string {
	return string(err)
}
