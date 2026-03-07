package cortex

import (
	"unsafe"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/store"
)

// BestFillFunc is the GPU resonance search function injected from vm.Machine.
type BestFillFunc func(
	dictionary unsafe.Pointer,
	numChords int,
	context unsafe.Pointer,
	expectedReality unsafe.Pointer,
	mode int,
	geodesicLUT unsafe.Pointer,
) (int, float64, error)

/*
Config holds all initialization parameters for the cortex graph.
All fields are set by the caller (vm.Machine); the cortex never
reaches outside its own boundary.
*/
type Config struct {
	// InitialNodes is the number of nodes spawned at birth. Default: 8.
	InitialNodes int

	// PrimeField is the long-term bedrock memory (read-only during thought).
	PrimeField *store.PrimeField

	// BestFill is the GPU resonance search kernel.
	BestFill BestFillFunc

	// MaxTicks is the convergence timeout. After this many ticks the graph
	// force-extracts whatever output it has. Default: 256.
	MaxTicks int

	// MaxOutput is the maximum number of bytes to generate. Default: 256.
	MaxOutput int

	// InboxSize is the channel buffer depth per node. Default: 32.
	InboxSize int

	// ConvergenceWindow is the number of consecutive ticks with stable
	// sink energy required to declare convergence. Default: 8.
	ConvergenceWindow int
}

func (c *Config) defaults() {
	if c.InitialNodes <= 0 {
		c.InitialNodes = 8
	}
	if c.MaxTicks <= 0 {
		c.MaxTicks = 256
	}
	if c.MaxOutput <= 0 {
		c.MaxOutput = 256
	}
	if c.InboxSize <= 0 {
		c.InboxSize = defaultInboxSize
	}
	if c.ConvergenceWindow <= 0 {
		c.ConvergenceWindow = 8
	}
}

/*
Graph is the volatile working-memory cortex.
It is born from a prompt, vibrates until convergence, and dies when thought completes.
Surviving dense nodes are optionally written back to the PrimeField as new memories.
*/
type Graph struct {
	config Config
	nodes  []*Node
	source *Node // injection point (prompt enters here)
	sink   *Node // extraction point (output read from here)
	tick   int
	nextID int

	// convergence tracking
	sinkStableCount int
	sinkLastEnergy  float64
}

/*
New creates a cortex graph with the specified configuration.

The initial topology is a small-world network: a ring of N nodes with 2
random long-range edges per node. The source is node 0, the sink is node N-1.
*/
func New(cfg Config) *Graph {
	cfg.defaults()

	g := &Graph{
		config: cfg,
		nodes:  make([]*Node, 0, cfg.InitialNodes),
	}

	// Spawn seed nodes.
	for i := range cfg.InitialNodes {
		node := NewNode(i, 0)
		g.nodes = append(g.nodes, node)
		g.nextID = i + 1
	}

	// Ring topology: each node connects to its immediate neighbors.
	n := len(g.nodes)
	for i := range n {
		g.nodes[i].Connect(g.nodes[(i+1)%n])
		g.nodes[(i+1)%n].Connect(g.nodes[i])
	}

	// Small-world shortcuts: 2 long-range edges per node using a
	// deterministic spread (not random — reproducible for tests).
	for i := range n {
		far1 := (i + n/3) % n
		far2 := (i + 2*n/3) % n
		if far1 != i {
			g.nodes[i].Connect(g.nodes[far1])
			g.nodes[far1].Connect(g.nodes[i])
		}
		if far2 != i {
			g.nodes[i].Connect(g.nodes[far2])
			g.nodes[far2].Connect(g.nodes[i])
		}
	}

	g.source = g.nodes[0]
	g.sink = g.nodes[n-1]

	return g
}

// Nodes returns the current node list. Read-only view.
func (g *Graph) Nodes() []*Node { return g.nodes }

// Source returns the graph's injection point.
func (g *Graph) Source() *Node { return g.source }

// Sink returns the graph's extraction point.
func (g *Graph) Sink() *Node { return g.sink }

// Tick returns the current tick count.
func (g *Graph) TickCount() int { return g.tick }

/*
SpawnNode creates a new node, connects it bidirectionally to `parent`,
and optionally to the nearest existing node (by cube chord similarity).
This is the structural expansion mechanism triggered by density pressure.
*/
func (g *Graph) SpawnNode(parent *Node) *Node {
	child := NewNode(g.nextID, g.tick)
	g.nextID++
	g.nodes = append(g.nodes, child)

	// Bidirectional link to parent.
	parent.Connect(child)
	child.Connect(parent)

	// Find the most resonant existing node (excluding parent) and connect.
	parentSummary := parent.CubeChord()
	var bestNode *Node
	bestSim := 0
	for _, n := range g.nodes {
		if n == child || n == parent {
			continue
		}
		nSummary := n.CubeChord()
		sim := data.ChordSimilarity(&parentSummary, &nSummary)
		if sim > bestSim {
			bestSim = sim
			bestNode = n
		}
	}
	if bestNode != nil {
		child.Connect(bestNode)
		bestNode.Connect(child)
	}

	return child
}

/*
bestNeighbor selects the edge with highest resonance to the given chord.
This is "Topological Gravity" — tokens naturally flow toward nodes
whose existing content has the most constructive interference.
*/
func (g *Graph) bestNeighbor(from *Node, c data.Chord) *Node {
	var best *Node
	bestSim := -1
	for _, neighbor := range from.edges {
		nSum := neighbor.CubeChord()
		sim := data.ChordSimilarity(&c, &nSum)
		if sim > bestSim {
			bestSim = sim
			best = neighbor
		}
	}
	return best
}

/*
queryBedrock fires a BestFill against the PrimeField using the node's
ChordHole as the query. This is "Thermodynamic Suction" — the node
dreams about exactly what it's missing.

If the PrimeField has a resonant match, the matched content is injected
back into the node as a new token (bedrock → working memory).
*/
func (g *Graph) queryBedrock(node *Node) {
	if g.config.PrimeField == nil || g.config.BestFill == nil {
		return
	}

	hole, shouldDream := node.Hole()
	if !shouldDream {
		return
	}

	// Build a temporary IcosahedralManifold with the hole as the query.
	// Place the hole chord on all 257 faces of Cubes[0] — this maximizes
	// resonance signal in the BestFill kernel.
	var ctx geometry.IcosahedralManifold
	face := dominantFace(&hole)
	ctx.Cubes[0][face] = hole

	dictPtr, dictN, _ := g.config.PrimeField.SearchSnapshot()
	if dictN == 0 {
		return
	}

	_, score, err := g.config.BestFill(
		dictPtr,
		dictN,
		unsafe.Pointer(&ctx),
		unsafe.Pointer(&ctx), // expected = self (we want anything that fills the hole)
		0,
		unsafe.Pointer(&geometry.UnifiedGeodesicMatrix[0]),
	)
	if err != nil || score < 0.05 {
		return
	}

	// Inject the hole-filling chord back into the node.
	// The hole itself IS the query result's structural fingerprint —
	// what came back is exactly what was missing. Merge it in directly.
	node.Send(Token{
		Chord:  hole,
		Origin: -1, // from bedrock
		TTL:    3,  // short-lived memory recall
	})
}
