package cortex

import (
	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/resonance"
)

/*
Arrive processes a token landing at this node. This is the heart of the cortex —
all "intelligence" happens here through bitwise interference, not if-statements.

Returns any tokens emitted as a consequence of the reaction, which the graph
ticker will route to neighbors.
*/
func (n *Node) Arrive(tok Token) []Token {
	n.traffic++

	// ── ROTATION (the "LAW" mechanism) ────────────────────────────────
	// A rotation token composes its GF(257) transform with the node's lens.
	// The node's perspective on reality shifts. Future incoming data
	// will land on different faces. The node has "learned" a new rule
	// without writing any symbolic logic.
	if tok.IsRotational() {
		incoming := tok.DecodeRotation()
		n.Rot = n.Rot.Compose(incoming)
		if event, ok := geometry.RotationEvent(incoming); ok {
			state := int(n.Header.RotState())
			if state >= 0 && state < len(geometry.StateTransitionMatrix) {
				next := geometry.StateTransitionMatrix[state][event]
				if next != 255 {
					n.Header.SetRotState(next)
				}
			}
			if n.Header.State() == 1 {
				n.Header.IncrementWinding()
			}
		}
		return nil // fully absorbed — no output
	}

	// ─────────────────────────────────────────────────────────────────
	logicalFace := tok.LogicalFace
	var emitted []Token
	routed := n.Rot.Forward(logicalFace)

	// Snapshot before merge (needed for interference detection).
	before := n.Cube[routed]

	// Merge via ChordOR — accumulative superposition.
	n.Cube[routed] = data.ChordOR(&n.Cube[routed], &tok.Chord)
	n.InvalidateChordCache()

	// ── DESTRUCTIVE INTERFERENCE → EMISSION ──────────────────────
	// Residues flow through the graph via topological gravity.
	hole := data.ChordHole(&tok.Chord, &before)
	if hole.ActiveCount() > 0 && tok.TTL > 1 {
		emitted = append(emitted, Token{
			Chord:       hole,
			LogicalFace: tok.LogicalFace,
			Origin:      n.ID,
			TTL:         tok.TTL - 1,
		})
	}

	// ── TRANSITIVE RESONANCE ─────────────────────────────────────────
	// Results of multi-hop reasoning are directed to LogicalFace 256
	// (the working register).
	summary := n.CubeChord()
	if summary.ActiveCount() > 5 && tok.Chord.ActiveCount() > 3 {
		shared := data.ChordGCD(&tok.Chord, &summary)
		if shared.ActiveCount() > 1 { // sufficient pairwise overlap
			faceContent := n.Cube[routed]
			h := resonance.TransitiveResonance(&tok.Chord, &summary, &faceContent)
			if h.ActiveCount() > 2 && tok.TTL > 1 {
				if tok.LogicalFace != 256 {
					console.Info("transitive resonance",
						"node", n.ID,
						"tok_face", tok.LogicalFace,
						"h_active", h.ActiveCount(),
					)
				}

				emitted = append(emitted, Token{
					Chord:       h,
					LogicalFace: 256, // Store in working register
					Origin:      n.ID,
					TTL:         tok.TTL - 1,
				})
			}
		}
	}

	// ── DENSITY-TRIGGERED MITOSIS (the "ARCHITECT" mechanism) ────────
	// When a face crosses the thermodynamic saturation threshold,
	// the node is overwhelmed. It emits a "pressure" signal carrying
	// the saturated face's full content.
	if n.FaceDensity(routed) >= geometry.MitosisThreshold {
		n.Header.SetState(1)
		emitted = append(emitted, Token{
			Chord:       n.Cube[routed],
			LogicalFace: tok.LogicalFace,
			Origin:      n.ID,
			TTL:         tok.TTL - 1,
		})
		// Retain only the stable overlap between prior state and the incoming
		// chord so saturated faces keep consensus structure rather than echoing
		// the entire bundle indefinitely.
		consensus := data.ChordGCD(&before, &tok.Chord)
		if consensus.ActiveCount() > 0 {
			n.Cube[routed] = consensus
		} else {
			n.Cube[routed] = data.Chord{}
		}
		n.InvalidateChordCache()
	}

	return emitted
}

/*
Hole computes the node's "curiosity" — the structural vacuum of its cube.
It returns the dominant face chord plus the remainder of the node summary
that the dominant face does not yet explain.
*/
func (n *Node) Hole() (data.Chord, data.Chord, int, bool) {
	summary := n.CubeChord()
	if summary.ActiveCount() == 0 {
		return data.Chord{}, data.Chord{}, 256, false
	}

	bestFaceIdx := n.bestPhysicalFace()
	if bestFaceIdx == 256 {
		return data.Chord{}, data.Chord{}, 256, false
	}

	peak := n.Cube[bestFaceIdx]
	hole := data.ChordHole(&summary, &peak)

	// Dream if the hole has meaningful structure AND the node has significant gaps.
	return peak, hole, bestFaceIdx, hole.ActiveCount() >= 3 && summary.ActiveCount() > peak.ActiveCount()
}

// bestPhysicalFace returns the raw physical face index with highest popcount.
// This is the internal version without rotation reversal.
func (n *Node) bestPhysicalFace() int {
	bestFace := 256
	bestCount := 0
	for i := range geometry.CubeFaces {
		cnt := n.Cube[i].ActiveCount()
		if cnt > bestCount {
			bestCount = cnt
			bestFace = i
		}
	}
	return bestFace
}
