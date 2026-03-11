package cortex

import (
	"fmt"
	"os"
	"sort"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/pool"
)

const (
	// entropyFloorQuietTicks is the number of ticks without a rotation before
	// thermal noise is injected. Prevents the substrate from freezing into
	// an inert crystal.
	entropyFloorQuietTicks = 40
)

/*
Step advances the cortex by one discrete time step.
*/
func (graph *Graph) Step() bool {
	graph.tick++
	graph.drainInboxes()
	graph.fireActiveEdges()
	graph.expandTopology()
	graph.promoteTools()
	graph.composeTools()
	graph.compileCircuits()
	graph.injectEntropyFloor()

	if graph.tick%16 == 0 {
		graph.prune()
	}

	converged := graph.checkConvergence()

	if converged && graph.broadcast != nil && !graph.outputEmitted {
		graph.outputEmitted = true
		graph.broadcast.Send(pool.NewResult(
			*pool.NewPoolValue(
				pool.WithKey[LogicSnapshot]("logic"),
				pool.WithValue(graph.SnapshotLogic()),
			),
		))
		graph.broadcast.Send(pool.NewResult(
			*pool.NewPoolValue(
				pool.WithKey[[]data.Chord]("results"),
				pool.WithValue(graph.extractResults()),
			),
		))
	}

	return converged
}

/*
drainInboxes processes all in-flight tokens across every node.
*/
func (graph *Graph) drainInboxes() {
	for _, node := range graph.nodes {
		for _, tok := range node.DrainInbox() {
			node.Arrive(tok)
		}
	}
}

/*
fireActiveEdges refreshes topology, ranks edge programs by activation weight,
and fires the hottest local logic circuits for this tick.
*/
func (graph *Graph) fireActiveEdges() {
	edges := graph.Edges()
	if len(edges) == 0 {
		return
	}

	type scoredEdge struct {
		edge  *Edge
		score float64
	}

	scored := make([]scoredEdge, 0, len(edges))

	for _, edge := range edges {
		edge.Refresh()
		if edge.Activation > 0 {
			scored = append(scored, scoredEdge{edge: edge, score: edge.Activation})
		}
	}

	sort.Slice(scored, func(left, right int) bool {
		return scored[left].score > scored[right].score
	})

	limit := max(1, len(scored)/3)
	activeEdges := make([]*Edge, 0, limit)
	for idx, entry := range scored {
		if idx < limit || entry.score >= 0.15 {
			activeEdges = append(activeEdges, entry.edge)
			continue
		}
		break
	}

	rotated := false

	for _, edge := range activeEdges {
		_, to, tok, ok := edge.Pulse()
		if !ok {
			continue
		}
		to.Arrive(tok)
		edge.TokensSent++

		if edge.Op.Band() == "rotate" {
			rotated = true
		}
	}

	if rotated {
		graph.lastRotationTick = graph.tick
	}
}

/*
expandTopology processes both search expansion and fork materialization.
Search grows or reconnects the graph. Fork compiles volatile registers that may
later be promoted into reusable tools.
*/
func (graph *Graph) expandTopology() {
	for _, node := range graph.nodes {
		if node.forkPending {
			graph.materializeFork(node)
		}

		if !node.searchPending {
			continue
		}

		node.searchPending = false

		if node.Energy() <= 0.1 {
			continue
		}

		searchChord := node.SearchChord()
		nearest := graph.NearestNode(node.Rot)

		if nearest != nil && nearest != node {
			nearestSummary := nearest.CubeChord()
			if nearest.Role == RoleTool && nearest.Interface.ActiveCount() > 0 {
				nearestSummary = data.ChordOR(&nearestSummary, &nearest.Interface)
			}

			if searchChord.ActiveCount() == 0 || data.ChordSimilarity(&searchChord, &nearestSummary) > 0 {
				node.Connect(nearest)
				nearest.Connect(node)
				continue
			}
		}

		graph.SpawnRegister(node, searchChord, searchChord, node.Program)
	}
}

/*
injectEntropyFloor injects a random rotation when the substrate has been
quiet for too long. Prevents crystallization into inert dead zones.
*/
func (graph *Graph) injectEntropyFloor() {
	quiet := graph.tick - graph.lastRotationTick

	if quiet <= entropyFloorQuietTicks || len(graph.nodes) == 0 {
		return
	}

	idx := int(rState % uint32(len(graph.nodes)))
	rState ^= rState << 13
	rState ^= rState >> 17
	rState ^= rState << 5

	axes := []Opcode{OpRotateX, OpRotateY, OpRotateZ}
	axis := axes[rState%3]

	var rot geometry.GFRotation

	switch axis {
	case OpRotateX:
		rot = geometry.DefaultRotTable.X90
	case OpRotateY:
		rot = geometry.DefaultRotTable.Y90
	default:
		rot = geometry.DefaultRotTable.Z90
	}

	target := graph.nodes[idx]
	target.Rot = target.Rot.Compose(rot)
	target.InvalidateChordCache()
	graph.lastRotationTick = graph.tick
}

// xorshift random for fast probabilistic routing
var rState uint32 = 2463534242

func fastRand() float64 {
	rState ^= rState << 13
	rState ^= rState >> 17
	rState ^= rState << 5

	return float64(rState&0xFFFFFF) / 16777216.0
}

func randShuffle(edges []*Edge) {
	for idx := len(edges) - 1; idx > 0; idx-- {
		rState ^= rState << 13
		rState ^= rState >> 17
		rState ^= rState << 5

		swapIdx := rState % uint32(idx+1)
		edges[idx], edges[swapIdx] = edges[swapIdx], edges[idx]
	}
}

/*
Edges returns all unique edges currently active in the graph.
*/
func (graph *Graph) Edges() []*Edge {
	var edgeList []*Edge
	seen := make(map[*Edge]bool)

	for _, node := range graph.nodes {
		for _, edge := range node.edges {
			if !seen[edge] {
				seen[edge] = true
				edgeList = append(edgeList, edge)
			}
		}
	}

	return edgeList
}

/*
prune removes energy-starved nodes that are not the source or sink.
*/
func (graph *Graph) prune() {
	const (
		starvationThreshold = 0.01
		gracePeriod         = 32
	)

	alive := make([]*Node, 0, len(graph.nodes))

	for _, node := range graph.nodes {
		if node == graph.source || node == graph.sink {
			alive = append(alive, node)
			continue
		}

		if node.Role == RoleTool {
			alive = append(alive, node)
			continue
		}

		age := graph.tick - node.birth
		threshold := starvationThreshold
		grace := gracePeriod
		if node.Role == RoleRegister {
			threshold *= 0.5
			grace /= 2
		}

		if age < grace || node.Energy() >= threshold || node.Support > 0 {
			alive = append(alive, node)
			continue
		}

		for _, edge := range node.edges {
			neighbor := edge.A

			if neighbor == node {
				neighbor = edge.B
			}

			pruneEdge(neighbor, node)
		}

		graph.pruneEvents++
	}

	graph.nodes = alive
	graph.reindexToolCatalog()
}

func pruneEdge(node, target *Node) {
	for idx, edge := range node.edges {
		if edge.A == target || edge.B == target {
			node.edges = append(node.edges[:idx], node.edges[idx+1:]...)
			return
		}
	}
}

const convergenceWindow = 8

/*
checkConvergence determines whether the graph has reached a stable state.
Convergence requires energy stability (±1% over convergenceWindow ticks).
*/
func (graph *Graph) checkConvergence() bool {
	sinkEnergy := graph.sink.Energy()
	minStableEnergy := 8.0 / float64(geometry.CubeFaces*257)

	delta := sinkEnergy - graph.sinkLastEnergy

	if delta < 0 {
		delta = -delta
	}

	energyStable := sinkEnergy >= minStableEnergy && delta < 0.01
	graph.sinkStableCount++

	if !energyStable {
		graph.sinkStableCount = 0
	}

	graph.sinkLastEnergy = sinkEnergy

	return graph.sinkStableCount >= convergenceWindow
}

/*
Survivors returns all nodes with energy above the given threshold.
*/
func (graph *Graph) Survivors(threshold float64) []*Node {
	var result []*Node

	for _, node := range graph.nodes {
		if node == graph.source || node == graph.sink {
			continue
		}

		if node.Energy() >= threshold {
			result = append(result, node)
		}
	}

	return result
}

/*
InjectChords sends each chord to the most resonant current binding target.
Pure control-plane chords still enter through the source as signals.
*/
func (graph *Graph) InjectChords(chords []data.Chord) {
	for _, chord := range chords {
		if chord.Has(256) && chord.ActiveCount() == 1 {
			graph.source.Send(NewSignalToken(chord, chord, -1))
			graph.seqPos++
			continue
		}

		target := graph.bindTarget(chord)
		if target == nil {
			target = graph.source
		}

		target.Send(NewDataToken(chord, chord.IntrinsicFace(), -1))
		graph.seqPos++
	}
}

func (graph *Graph) LogTrace() {
	f, err := os.OpenFile("cortex_trace.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	if graph.tick == 1 {
		fmt.Fprintln(f, "==================================================")
		fmt.Fprintln(f, "NEW EXPERIMENT PROMPT CYCLE")
	}

	fmt.Fprintf(f, "TICK: %d | NODES: %d | EDGES: %d\n", graph.tick, len(graph.nodes), len(graph.Edges()))
	for _, node := range graph.nodes {
		chord := node.CubeChord()
		fmt.Fprintf(f, "  Node %d: Role=%s, Energy=%.3f, ChordActive=%d, Support=%d\n", node.ID, node.Role.String(), node.Energy(), chord.ActiveCount(), node.Support)
	}
}
