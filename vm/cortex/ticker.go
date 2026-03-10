package cortex

import (
	"fmt"
	"os"

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
	graph.injectEntropyFloor()
	graph.LogTrace()

	if graph.tick%16 == 0 {
		graph.prune()
	}

	converged := graph.checkConvergence()

	if converged && graph.broadcast != nil {
		var res []data.Chord
		var queryNode *Node

		// Find the last node that received data (the Query node)
		for i := len(graph.nodes) - 1; i >= 0; i-- {
			if graph.nodes[i] != graph.source && graph.nodes[i] != graph.sink {
				cc := graph.nodes[i].CubeChord()
				if cc.ActiveCount() > 0 {
					queryNode = graph.nodes[i]
					break
				}
			}
		}

		if queryNode != nil {
			var bestEdge *Edge
			var maxSim int

			// Find the most resonant neighbor (the target statement)
			for _, ed := range queryNode.edges {
				sim := ed.ChannelMask.ActiveCount()
				if sim > maxSim {
					maxSim = sim
					bestEdge = ed
				}
			}

			if bestEdge != nil {
				targetNode := bestEdge.A
				if targetNode == queryNode {
					targetNode = bestEdge.B
				}

				// Pure Novelty Extraction:
				// Subtract structural overlaps from ALL nodes in the graph to shed verbs, stop words, and entities,
				// leaving only the completely unique geometric signature of the node (the novel location).
				summary := targetNode.CubeChord()
				var structuralMask data.Chord

				for _, otherNode := range graph.nodes {
					if otherNode != targetNode && otherNode != graph.source && otherNode != graph.sink {
						otherSummary := otherNode.CubeChord()
						overlap := data.ChordAND(&summary, &otherSummary)
						structuralMask = data.ChordOR(&structuralMask, &overlap)
					}
				}

				ansResidue := data.ChordHole(&summary, &structuralMask)
				res = append(res, ansResidue)
			}
		}

		// Fallback to sink gate if resonance failed
		if len(res) == 0 {
			for side := range 6 {
				for rot := range 4 {
					chord := graph.sink.Cube.Get(side, rot, graph.sink.Rot.Forward(256))
					if chord.ActiveCount() > 0 {
						res = append(res, chord)
					}
				}
			}
		}

		graph.broadcast.Send(pool.NewResult(
			*pool.NewPoolValue(
				pool.WithKey[[]data.Chord]("results"),
				pool.WithValue(res),
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
fireActiveEdges refreshes topology, selects edges probabilistically,
and flows tokens downhill based on face 256 gradients.
*/
func (graph *Graph) fireActiveEdges() {
	edges := graph.Edges()
	var activeEdges []*Edge

	for _, edge := range edges {
		edge.Refresh()

		band := edge.Op.Band()
		weight := 0.005

		switch band {
		case "rotate":
			weight = 0.05
		case "growth":
			weight = 0.02
		}

		probability := weight + (1.0 / (1.0 + float64(edge.StableFrames)*0.02))

		if fastRand() < probability {
			activeEdges = append(activeEdges, edge)
		}
	}

	randShuffle(activeEdges)

	rotated := false

	for _, edge := range activeEdges {
		faceA := edge.A.Rot.Reverse(256)
		faceB := edge.B.Rot.Reverse(256)

		var from, to *Node
		var toGate data.Chord

		if faceA > faceB {
			from, to = edge.A, edge.B
			toGate = edge.B.Cube.Get(edge.PatchB.Side, edge.PatchB.Rot, edge.B.Rot.Forward(256))
		} else {
			from, to = edge.B, edge.A
			toGate = edge.A.Cube.Get(edge.PatchA.Side, edge.PatchA.Rot, edge.A.Rot.Forward(256))
		}

		payload := from.CubeChord()
		if toGate.ActiveCount() > 0 {
			filtered := data.ChordAND(&payload, &toGate)
			if filtered.ActiveCount() == 0 {
				continue // Gate is physically blocking this data shape
			}
			payload = filtered // Only the exact matching geometry passes
		}

		tok := Token{
			Chord:       payload,
			LogicalFace: 256,
			Origin:      from.ID,
			TTL:         1,
			Op:          edge.Op,
			Carry:       from.Rot,
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
expandTopology processes nodes flagged by OpSearch.
Uses the kernel backend (GPU) for nearest rotational neighbor when available.
Falls back to SpawnNode otherwise.
*/
func (graph *Graph) expandTopology() {
	for _, node := range graph.nodes {
		if !node.searchPending {
			continue
		}

		node.searchPending = false

		if node.Energy() <= 0.1 {
			continue
		}

		nearest := graph.NearestNode(node.Rot)

		if nearest != nil && nearest != node {
			node.Connect(nearest)
			nearest.Connect(node)
		} else {
			graph.SpawnNode(node)
		}
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

		age := graph.tick - node.birth

		if age < gracePeriod || node.Energy() >= starvationThreshold {
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

func (graph *Graph) InjectChords(chords []data.Chord) {
	targetIdx := 0
	currentTarget := graph.nodes[targetIdx]

	for _, chord := range chords {
		currentTarget.Send(NewDataToken(chord, chord.IntrinsicFace(), -1))
		graph.seqPos++

		face := chord.IntrinsicFace()
		if face == '.' || face == '?' || face == '\n' {
			targetIdx++
			if targetIdx >= len(graph.nodes)-1 { // leave sink alone if possible
				graph.SpawnNode(currentTarget)
			}
			if targetIdx < len(graph.nodes) {
				currentTarget = graph.nodes[targetIdx]
			}
		}
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
		c := node.CubeChord()
		fmt.Fprintf(f, "  Node %d: Energy=%.3f, ChordActive=%d\n", node.ID, node.Energy(), c.ActiveCount())
	}
}
