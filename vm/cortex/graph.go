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
Graph is the cortex: source/sink nodes, ring+small-world topology.
Runs Step() until convergence; all external communication goes through the pool.
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

	sinkStableCount int
	sinkLastEnergy  float64

	seqPos uint32
	seqZ   uint8

	momentum         float64
	lastRotationTick int

	bedrockQueries int
	mitosisEvents  int
	pruneEvents    int

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
		nodes:    make([]*Node, 0, 8),
		momentum: 0.0,
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

	for i := range config.Cortex.InitialNodes {
		node := NewNode(i, 0)
		graph.nodes = append(graph.nodes, node)
		graph.nextID = i + 1
	}

	nodeCount := len(graph.nodes)

	for i := range nodeCount {
		graph.nodes[i].Connect(graph.nodes[(i+1)%nodeCount])
		graph.nodes[(i+1)%nodeCount].Connect(graph.nodes[i])
	}

	for i := range nodeCount {
		far1 := (i + nodeCount/3) % nodeCount
		far2 := (i + 2*nodeCount/3) % nodeCount

		if far1 != i {
			graph.nodes[i].Connect(graph.nodes[far1])
			graph.nodes[far1].Connect(graph.nodes[i])
		}

		if far2 != i {
			graph.nodes[i].Connect(graph.nodes[far2])
			graph.nodes[far2].Connect(graph.nodes[i])
		}
	}

	graph.source = graph.nodes[0]
	graph.sink = graph.nodes[nodeCount-1]

	return graph
}

/*
Tick processes a PoolValue and steps the cortex.
*/
func (graph *Graph) Tick(result *pool.Result) {
	if result != nil && result.Value != nil {
		if pv, ok := result.Value.(pool.PoolValue[[]data.Chord]); ok {
			if pv.Key == "prompt" {
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
SpawnNode creates a new node, connects it bidirectionally to parent,
and to the most resonant existing node by cube chord similarity.
*/
func (graph *Graph) SpawnNode(parent *Node) *Node {
	child := NewNode(graph.nextID, graph.tick)
	graph.nextID++
	graph.nodes = append(graph.nodes, child)
	graph.mitosisEvents++

	parent.Connect(child)
	child.Connect(parent)

	parentSummary := parent.CubeChord()
	var bestNode *Node
	bestSim := 0

	for _, candidate := range graph.nodes {
		if candidate == child || candidate == parent {
			continue
		}

		candidateSummary := candidate.CubeChord()
		sim := data.ChordSimilarity(&parentSummary, &candidateSummary)

		if sim > bestSim {
			bestSim = sim
			bestNode = candidate
		}
	}

	if bestNode != nil {
		child.Connect(bestNode)
		bestNode.Connect(child)
	}

	return child
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
			for i := 0; i < 257; i++ {
				node.Cube.Set(side, rot, i, data.Chord{})
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
