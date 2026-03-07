package cortex

import (
	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
)

/*
Tick advances the cortex by one discrete time step.

Each tick has five phases:

 1. DRAIN — All nodes drain their inboxes.
 2. REACT — Each node processes arrivals (accumulation, interference, rotation).
 3. ROUTE — Emitted tokens are routed to neighbors via topological gravity.
 4. DREAM — Nodes with curiosity (ChordHole) query the PrimeField.
 5. PRUNE — Energy-starved nodes are removed; convergence is checked.

Returns true if the graph has converged (sink energy is stable).
*/
func (g *Graph) Tick() bool {
	g.tick++

	// ── Phase 1+2: DRAIN & REACT ──────────────────────────────────
	// Each node drains its inbox and processes arrivals in isolation.
	// This is safe to parallelize since each node owns its cube exclusively.
	type emission struct {
		from   *Node
		tokens []Token
	}
	emissions := make([]emission, 0, len(g.nodes))

	for _, node := range g.nodes {
		drained := node.DrainInbox()
		if len(drained) == 0 {
			continue
		}

		var allEmitted []Token
		for _, tok := range drained {
			emitted := node.Arrive(tok)
			allEmitted = append(allEmitted, emitted...)
		}

		if len(allEmitted) > 0 {
			emissions = append(emissions, emission{from: node, tokens: allEmitted})
		}
	}

	// ── Phase 3: ROUTE ────────────────────────────────────────────
	// Route emitted tokens to the best neighbor via resonance gravity.
	// Mitosis tokens (from density-triggered emissions) also spawn new nodes.
	for _, em := range emissions {
		for _, tok := range em.tokens {
			if tok.TTL <= 0 {
				continue
			}

			// If the emitting node has very high density on the token's
			// dominant face, this is a mitosis event → spawn a new node.
			face := dominantFace(&tok.Chord)
			if em.from.FaceDensity(face) < 0.01 && tok.Chord.ActiveCount() > 20 {
				// High-content token from a nearly-empty face = mitosis debris.
				child := g.SpawnNode(em.from)
				child.Send(tok)
				continue
			}

			// Normal routing: topological gravity.
			neighbor := g.bestNeighbor(em.from, tok.Chord)
			if neighbor != nil {
				neighbor.Send(tok)
			}
		}
	}

	// ── Phase 4: DREAM ────────────────────────────────────────────
	// Curiosity-driven bedrock queries. Only check every 4 ticks
	// to avoid GPU over-saturation.
	if g.tick%4 == 0 {
		for _, node := range g.nodes {
			g.queryBedrock(node)
		}
	}

	// ── Phase 5: PRUNE & CONVERGE ─────────────────────────────────
	if g.tick%16 == 0 {
		g.prune()
	}

	return g.checkConvergence()
}

/*
prune removes energy-starved nodes that are not the source or sink.
A node is starved if its total popcount density falls below 1% AND
it has been alive for at least 32 ticks (grace period for newly spawned nodes).
*/
func (g *Graph) prune() {
	const (
		starvationThreshold = 0.01
		gracePeriod         = 32
	)

	alive := make([]*Node, 0, len(g.nodes))
	for _, node := range g.nodes {
		// Never prune source or sink.
		if node == g.source || node == g.sink {
			alive = append(alive, node)
			continue
		}
		age := g.tick - node.birth
		if age < gracePeriod || node.Energy() >= starvationThreshold {
			alive = append(alive, node)
			continue
		}
		// Node dies: disconnect from all neighbors.
		for _, neighbor := range node.edges {
			pruneEdge(neighbor, node)
		}
		console.Info("cortex prune",
			"nodeID", node.ID,
			"energy", node.Energy(),
			"age", age,
		)
	}
	g.nodes = alive
}

// pruneEdge removes `target` from `node`'s edge list.
func pruneEdge(node, target *Node) {
	for i, e := range node.edges {
		if e == target {
			node.edges = append(node.edges[:i], node.edges[i+1:]...)
			return
		}
	}
}

/*
checkConvergence determines whether the graph has reached a stable state.
Convergence is defined as the sink node's energy remaining within ±1%
for ConvergenceWindow consecutive ticks.

This ensures the output is only extracted when the graph has stopped
vibrating — all contradictions have been resolved and the interference
pattern has settled.
*/
func (g *Graph) checkConvergence() bool {
	sinkEnergy := g.sink.Energy()

	// Check if energy is stable (delta < 1%)
	delta := sinkEnergy - g.sinkLastEnergy
	if delta < 0 {
		delta = -delta
	}

	if sinkEnergy > 0 && delta < 0.01 {
		g.sinkStableCount++
	} else {
		g.sinkStableCount = 0
	}

	g.sinkLastEnergy = sinkEnergy
	return g.sinkStableCount >= g.config.ConvergenceWindow
}

/*
Survivors returns all nodes with energy above the given threshold.
These are candidates for writing back to the PrimeField as new long-term
memories after the graph dissolves.

The returned slice excludes the source and sink nodes (their content
is transient prompt/output context, not learned knowledge).
*/
func (g *Graph) Survivors(threshold float64) []*Node {
	var result []*Node
	for _, node := range g.nodes {
		if node == g.source || node == g.sink {
			continue
		}
		if node.Energy() >= threshold {
			result = append(result, node)
		}
	}
	return result
}

/*
InjectChords feeds a sequence of chords into the source node as data tokens.
This is how the prompt enters the cortex.
*/
func (g *Graph) InjectChords(chords []data.Chord) {
	for _, c := range chords {
		g.source.Send(NewDataToken(c, -1))
	}
}
