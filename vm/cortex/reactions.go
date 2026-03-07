package cortex

import (
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
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
		return nil // fully absorbed — no output
	}

	// ── SELF-ADDRESSED ACCUMULATION ───────────────────────────────────
	// Determine which face this chord lands on by finding the chord's
	// dominant activation, then routing through the node's rotational lens.
	face := dominantFace(&tok.Chord)
	routed := n.Rot.Forward(face)

	// Snapshot before merge (needed for interference detection).
	before := n.Cube[routed]

	// Merge via ChordOR — accumulative superposition.
	n.Cube[routed] = data.ChordOR(&n.Cube[routed], &tok.Chord)

	var emitted []Token

	// ── DESTRUCTIVE INTERFERENCE → EMISSION ──────────────────────────
	// ChordHole computes what the incoming chord contributes that the
	// existing state contradicts: target AND NOT existing.
	// If there IS a hole, the contradiction signal propagates outward.
	// This is how the graph "disagrees" — contradictions are not suppressed;
	// they flow to neighbors for resolution.
	hole := data.ChordHole(&tok.Chord, &before)
	if hole.ActiveCount() > 0 && tok.TTL > 1 {
		emitted = append(emitted, Token{
			Chord:  hole,
			Origin: n.ID,
			TTL:    tok.TTL - 1,
		})
	}

	// ── DENSITY-TRIGGERED MITOSIS (the "ARCHITECT" mechanism) ────────
	// When a face crosses the thermodynamic saturation threshold,
	// the node is overwhelmed. It emits a "pressure" signal carrying
	// the saturated face's full content. The graph ticker will use
	// this to spawn a new node (structural growth from density pressure).
	if n.FaceDensity(routed) >= geometry.MitosisThreshold {
		emitted = append(emitted, Token{
			Chord:  n.Cube[routed],
			Origin: n.ID,
			TTL:    tok.TTL - 1,
		})
		// Partially drain the saturated face to relieve pressure.
		// Keep only the shared harmonics (GCD of before and after).
		n.Cube[routed] = data.ChordGCD(&before, &tok.Chord)
	}

	return emitted
}

/*
Hole computes the node's "curiosity" — the structural vacuum of its cube.

For each face, the hole is what a fully active reference chord has that
this face does not. The aggregate hole across all faces represents what
the node is "missing" and can be used as a BestFill query to the PrimeField.

Returns a chord representing the node's total informational deficit,
and a boolean indicating whether the deficit is significant enough
to warrant a bedrock query.
*/
func (n *Node) Hole() (data.Chord, bool) {
	// Build a reference chord from the node's own summary.
	summary := n.CubeChord()
	if summary.ActiveCount() == 0 {
		return data.Chord{}, false
	}

	// The hole is the complement: what the summary COULD have but doesn't.
	// We take the most active face as the "expected topology" and compute
	// What it lacks using ChordHole against the summary.
	bestFace := n.BestFace()
	if bestFace == 256 {
		return data.Chord{}, false
	}

	peak := n.Cube[bestFace]
	hole := data.ChordHole(&peak, &summary)

	// Threshold: only dream if the hole has meaningful structure
	// (at least 3 active bits — avoid noise-driven queries).
	return hole, hole.ActiveCount() >= 3
}

/*
dominantFace returns the face index (0–256) that best matches a chord
according to the Fermat cube self-addressing scheme.

For a BaseChord(b), this naturally returns a position related to b.
For composed chords, we use the ChordBin function to deterministically
fold the chord into a face index within the 257-face range.
*/
func dominantFace(c *data.Chord) int {
	bin := data.ChordBin(c)
	// ChordBin returns 0..255 — map into the 257-face space.
	// Values 0-255 map to data faces; 256 is reserved for delimiter.
	if bin < 0 {
		bin = 0
	}
	return bin % geometry.CubeFaces
}
