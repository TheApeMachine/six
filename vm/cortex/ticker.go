package cortex

import (
	"math"
	"sort"

	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

/*
Tick advances the cortex by one discrete time step.

Each tick has six phases:

 1. DRAIN — All nodes drain their inboxes.
 2. REACT — Each node processes arrivals (accumulation, interference, rotation).
 3. ROUTE — Emitted tokens are routed to neighbors via topological gravity.
 4. SEQUENCE — Source node chords are analyzed for topological events;
    events become LAW (rotation) tokens injected into the graph.
 5. DREAM — Nodes with curiosity (ChordHole) query the PrimeField.
 6. PRUNE — Energy-starved nodes are removed; convergence is checked.

Returns true if the graph has converged (sink energy is stable).
*/
func (g *Graph) Tick() bool {
	g.tick++

	// ── Phase 1+2: DRAIN & REACT ──────────────────────────────────
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
	for _, em := range emissions {
		for _, tok := range em.tokens {
			if tok.TTL <= 0 {
				continue
			}

			// If the emitting node has very high density on the token's
			// dominant face, this is a mitosis event → spawn a new node.
			face := tok.Chord.IntrinsicFace()
			if face < 257 && em.from.FaceDensity(face) < 0.01 && tok.Chord.ActiveCount() > 20 {
				child := g.SpawnNode(em.from)
				child.Send(tok)
				continue
			}

			// Normal routing: topological gravity.
			// When scores are degenerate (unstructured medium), broadcast
			// to all neighbors — waves spread omnidirectionally until the
			// medium develops structure to guide them.
			targets := g.routeTargets(em.from, tok.Chord)
			for _, target := range targets {
				target.Send(tok)
			}
		}
	}

	// ── Phase 4: SEQUENCE ─────────────────────────────────────────
	// Analyze the source node's current state through the Sequencer to
	// derive topological events. Each event becomes a LAW (rotation) token
	// that propagates through the graph, shifting node perspectives.
	if g.config.Sequencer != nil && g.tick%2 == 0 {
		sourceChord := g.source.CubeChord()
		if sourceChord.ActiveCount() > 0 {
			reset, events := g.config.Sequencer.Analyze(int(g.seqPos), byte(g.source.BestFace()&0xFF))

			for _, ev := range events {
				rot := geometry.EventRotation(ev)
				lawTok := NewRotationToken(rot, -1)
				// Broadcast LAW to all nodes except the sink — the sink is
				// an observation point whose rotation must stay stable.
				for _, node := range g.nodes {
					if node == g.sink {
						continue
					}
					node.Send(lawTok)
				}
			}

			g.seqPos++
			if reset {
				g.seqPos = 0
				g.seqZ++
			}
		}
	}

	// ── Phase 5: DREAM ────────────────────────────────────────────
	if g.tick%4 == 0 {
		if g.config.EigenMode != nil {
			// Phase-directed dreaming: nodes most out of phase dream first.
			allChords := make([]data.Chord, 0, len(g.nodes))
			for _, n := range g.nodes {
				allChords = append(allChords, n.CubeChord())
			}
			globalTheta, _ := g.config.EigenMode.SeqToroidalMeanPhase(allChords)

			type dreamCand struct {
				node *Node
				dev  float64
			}
			var cands []dreamCand
			for _, n := range g.nodes {
				chord := n.CubeChord()
				nodeTheta, _ := g.config.EigenMode.PhaseForChord(&chord)
				dev := math.Abs(nodeTheta - globalTheta)
				for dev > math.Pi {
					dev = 2*math.Pi - dev
				}
				cands = append(cands, dreamCand{node: n, dev: dev})
			}
			sort.Slice(cands, func(i, j int) bool {
				return cands[i].dev > cands[j].dev
			})

			for _, cand := range cands {
				g.queryBedrock(cand.node)
			}
		} else {
			for _, node := range g.nodes {
				g.queryBedrock(node)
			}
		}
	}

	// ── Phase 6: PRUNE & CONVERGE ─────────────────────────────────
	if g.tick%16 == 0 {
		g.prune()
	}

	return g.checkConvergence()
}

/*
prune removes energy-starved nodes that are not the source or sink.
*/
func (g *Graph) prune() {
	const (
		starvationThreshold = 0.01
		gracePeriod         = 32
	)

	alive := make([]*Node, 0, len(g.nodes))
	for _, node := range g.nodes {
		if node == g.source || node == g.sink {
			alive = append(alive, node)
			continue
		}
		age := g.tick - node.birth
		if age < gracePeriod || node.Energy() >= starvationThreshold {
			alive = append(alive, node)
			continue
		}
		for _, neighbor := range node.edges {
			pruneEdge(neighbor, node)
		}
		g.pruneEvents++
		console.Info("cortex prune",
			"nodeID", node.ID,
			"energy", node.Energy(),
			"age", age,
		)
	}
	g.nodes = alive
}

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
Convergence requires energy stability (±1% over ConvergenceWindow ticks)
and, if EigenMode is active, Toroidal Closure.
*/
func (g *Graph) checkConvergence() bool {
	sinkEnergy := g.sink.Energy()
	minStableEnergy := 8.0 / float64(geometry.CubeFaces*257)

	delta := sinkEnergy - g.sinkLastEnergy
	if delta < 0 {
		delta = -delta
	}

	energyStable := sinkEnergy >= minStableEnergy && delta < 0.01

	if energyStable {
		g.sinkStableCount++
	} else {
		g.sinkStableCount = 0
	}

	g.sinkLastEnergy = sinkEnergy
	stable := g.sinkStableCount >= g.config.ConvergenceWindow

	// Toroidal Closure: the sink's phase must match the global graph baseline.
	if stable && g.config.EigenMode != nil {
		allChords := make([]data.Chord, 0, len(g.nodes))
		for _, n := range g.nodes {
			allChords = append(allChords, n.CubeChord())
		}
		globalAnchor, _ := g.config.EigenMode.SeqToroidalMeanPhase(allChords)
		sinkChord := g.sink.CubeChord()

		closed := g.config.EigenMode.IsGeometricallyClosed([]data.Chord{sinkChord}, globalAnchor)
		if !closed {
			return false
		}
	}

	return stable
}

/*
Survivors returns all nodes with energy above the given threshold.
These are candidates for writing back to the PrimeField.
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
		positioned := c.RollLeft(int(g.seqPos))
		g.source.Send(NewDataToken(positioned, c.IntrinsicFace(), -1))
		g.seqPos++
	}
}

/*
InjectWithSequencer feeds prompt chords into the source node AND runs each
through the Sequencer to generate topological events. Events become LAW
(rotation) tokens injected alongside data tokens — the graph experiences
both the content and the structural dynamics of the prompt.
*/
func (g *Graph) InjectWithSequencer(chords []data.Chord) {
	for _, c := range chords {
		positioned := c.RollLeft(int(g.seqPos))
		reset := false

		if g.config.Sequencer != nil {
			var events []int
			reset, events = g.config.Sequencer.Analyze(int(g.seqPos), byte(c.IntrinsicFace()&0xFF))
			g.accumulateMomentum(positioned, events)
			for _, ev := range events {
				rot := geometry.EventRotation(ev)
				lawTok := NewRotationToken(rot, -1)
				for _, node := range g.nodes {
					if node == g.sink {
						continue
					}
					node.Send(lawTok)
				}
			}
		}

		g.source.Send(NewDataToken(positioned, c.IntrinsicFace(), -1))
		g.seqPos++

		if reset {
			g.seqPos = 0
			g.seqZ++
		}
	}
}

func (g *Graph) accumulateMomentum(chord data.Chord, events []int) {
	if len(events) == 0 {
		return
	}

	g.momentum += float64(chord.ActiveCount())
}
