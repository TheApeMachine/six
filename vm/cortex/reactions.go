package cortex

import (
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

	// ── SELF-ADDRESSED ACCUMULATION ───────────────────────────────────
	// Determine which face this chord lands on using the Fermat cube
	// self-addressing property (byte value = face), then route through
	// the node's rotational lens.
	logicalFace := tok.Chord.IntrinsicFace()
	if logicalFace == 256 {
		return nil
	}

	var emitted []Token
	routed := n.Rot.Forward(logicalFace)

	// Snapshot before merge (needed for interference detection).
	before := n.Cube[routed]

	// Merge via ChordOR — accumulative superposition.
	n.Cube[routed] = data.ChordOR(&n.Cube[routed], &tok.Chord)

	// ── DESTRUCTIVE INTERFERENCE → EMISSION ──────────────────────
	hole := data.ChordHole(&tok.Chord, &before)
	if hole.ActiveCount() > 0 && tok.TTL > 1 {
		emitted = append(emitted, Token{
			Chord:  hole,
			Origin: n.ID,
			TTL:    tok.TTL - 1,
		})
	}

	// ── TRANSITIVE RESONANCE ─────────────────────────────────────────
	// When a node has accumulated enough context (CubeChord) and the
	// incoming token shares structure with it, fire TransitiveResonance
	// to produce analogical inferences — the (A:B::C:D) operation.
	//
	// F1 = incoming chord, F2 = node's summary, F3 = face content after merge.
	// H = the bits F1 uniquely contributes beyond shared context, PLUS
	//     the bits the face content uniquely contributes beyond shared context.
	// This is multi-hop reasoning as an emergent property of interference.
	summary := n.CubeChord()
	if summary.ActiveCount() > 5 && tok.Chord.ActiveCount() > 3 {
		shared := data.ChordGCD(&tok.Chord, &summary)
		if shared.ActiveCount() > 1 { // sufficient pairwise overlap
			faceContent := n.Cube[routed]
			h := resonance.TransitiveResonance(&tok.Chord, &summary, &faceContent)
			if h.ActiveCount() > 2 && tok.TTL > 1 {
				emitted = append(emitted, Token{
					Chord:  h,
					Origin: n.ID,
					TTL:    tok.TTL - 1,
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
			Chord:  n.Cube[routed],
			Origin: n.ID,
			TTL:    tok.TTL - 1,
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
