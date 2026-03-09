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
func (n *Node) Arrive(tok Token) {
	n.traffic++

	if tok.IsSignal {
		n.Signals = append(n.Signals, tok)
		// Computation bypasses the attractor. Thread the needle.
		if tok.TTL <= 1 {
			return // signal dies
		}

		for _, ed := range n.edges {
			// Don't bounce back to origin
			neighbor := ed.A
			if neighbor == n {
				neighbor = ed.B
			}

			if neighbor.ID == tok.Origin {
				continue
			}

			overlap := data.ChordAND(&tok.SignalMask, &ed.ChannelMask)
			if overlap.ActiveCount() > 0 {
				neighbor.Send(Token{
					Chord:       tok.Chord,
					LogicalFace: tok.LogicalFace,
					Origin:      n.ID, // Update origin so it doesn't bounce back next hop
					TTL:         tok.TTL - 1,
					Op:          tok.Op,
					Carry:       tok.Carry,
					IsSignal:    true,
					SignalMask:  tok.SignalMask,
				})
			}
		}

		return
	}

	switch tok.Op {
	case OpRotateX:
		n.Rot = n.Rot.Compose(geometry.DefaultRotTable.X90)
	case OpRotateY:
		n.Rot = n.Rot.Compose(geometry.DefaultRotTable.Y90)
	case OpRotateZ:
		n.Rot = n.Rot.Compose(geometry.DefaultRotTable.Z90)

	case OpAlign, OpCompose, OpFork:
		n.Rot = n.Rot.Compose(tok.Carry)

	case OpSync:
		a := max((int(n.Rot.A)+int(tok.Carry.A))/2, 1)
		n.Rot.A = uint16(a)
		n.Rot.B = uint16((int(n.Rot.B) + int(tok.Carry.B)) / 2 % 257)

	case OpSearch:
		n.Rot = n.Rot.Compose(tok.Carry)
		n.searchPending = true
	}

	// Token data is absorbed directly into the MacroCube
	// guided by the node's GF(257) lens.
	routed := n.Rot.Forward(tok.LogicalFace)
	n.Cube[routed] = data.ChordOR(&n.Cube[routed], &tok.Chord)
	n.InvalidateChordCache()
}

/*
Hole returns (peak, hole, bestFaceIdx, shouldDream). peak = highest-popcount face;
hole = ChordHole(summary, peak). shouldDream when hole has ≥3 bits and summary > peak.
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

/*
bestPhysicalFace returns the physical face index (0-257) with highest ActiveCount.
No rotation reversal (unlike BestFace).
*/
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
