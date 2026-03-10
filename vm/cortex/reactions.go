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

		// 1. Memory Resonance (The Deduction Phase)
		// A signal interacts with the node's crystallized mass.
		cubeChord := n.CubeChord()
		sim := data.ChordSimilarity(&tok.Chord, &cubeChord)

		// If they share structural primes (e.g. 'Sandra'), they resonate.
		// We only react if the node has novel context to offer (it's not just a blank relay).
		if sim > 0 && cubeChord.ActiveCount() > tok.Chord.ActiveCount() {
			// Physical deduction: extract the context present in the memory but missing in the query.
			reaction := data.ChordHole(&cubeChord, &tok.Chord)

			if reaction.ActiveCount() > 0 {
				// The reaction produces a Reflection Signal containing the answer (e.g. 'Garden')
				reflection := NewSignalToken(reaction, reaction, n.ID)
				reflection.TTL = tok.TTL // Inherit remaining kinetic energy

				// Propagate the reflection outward
				for _, ed := range n.edges {
					neighbor := ed.A
					if neighbor == n {
						neighbor = ed.B
					}
					if neighbor.ID != tok.Origin {
						neighbor.Send(reflection)
					}
				}
			}
		}

		if tok.TTL <= 1 {
			return // signal dies
		}

		// 2. Wave Propagation
		// Forward the original query signal along open channels.
		for _, ed := range n.edges {
			neighbor := ed.A
			if neighbor == n {
				neighbor = ed.B
			}
			if neighbor.ID == tok.Origin {
				continue
			}

			// Diffuse along compatible channels or open unstructured space
			overlap := data.ChordAND(&tok.SignalMask, &ed.ChannelMask)
			controlOpen := false
			if tok.SignalMask.ActiveCount() == 1 && tok.SignalMask.Has(256) {
				controlOpen = ed.ControlMask.ActiveCount() > 0
			}

			if overlap.ActiveCount() > 0 || controlOpen || ed.ChannelMask.ActiveCount() == 0 {
				forward := tok
				forward.TTL--
				forward.Origin = n.ID
				neighbor.Send(forward)
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

	// Token data is absorbed directly into the Cube
	// guided by the node's GF(257) lens.
	routed := n.Rot.Forward(tok.LogicalFace)
	for side := 0; side < 6; side++ {
		for rot := 0; rot < 4; rot++ {
			c := n.Cube.Get(side, rot, routed)
			n.Cube.Set(side, rot, routed, data.ChordOR(&c, &tok.Chord))
		}
	}
	n.InvalidateChordCache()

	_, hole, _, shouldDream := n.Hole()
	if shouldDream {
		routedGate := n.Rot.Forward(256)
		for side := 0; side < 6; side++ {
			for rot := 0; rot < 4; rot++ {
				c := n.Cube.Get(side, rot, routedGate)
				n.Cube.Set(side, rot, routedGate, data.ChordOR(&c, &hole))
			}
		}
		n.InvalidateChordCache()
	}
}

/*
Hole computes the geometric residue between the node's total mass and the
Topological Intersection (anchor) with its neighbors.
Returns (anchor, residue, bestFaceIdx, shouldDream)
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

	var bestAnchor data.Chord
	var bestResidue data.Chord
	var bestAnchorSize int

	for _, ed := range n.edges {
		neighbor := ed.A
		if neighbor == n {
			neighbor = ed.B
		}

		neighborSummary := neighbor.CubeChord()
		anchor := data.ChordAND(&summary, &neighborSummary)
		residue := data.ChordHole(&summary, &anchor)

		anchorSize := anchor.ActiveCount()
		if anchorSize > bestAnchorSize && residue.ActiveCount() > 0 {
			bestAnchorSize = anchorSize
			bestAnchor = anchor
			bestResidue = residue
		}
	}

	return bestAnchor, bestResidue, bestFaceIdx, bestAnchorSize >= 3 && bestResidue.ActiveCount() > 0
}

/*
bestPhysicalFace returns the physical face index (0-257) with highest aggregate ActiveCount.
*/
func (n *Node) bestPhysicalFace() int {
	bestFace := 256
	bestCount := 0
	for i := 0; i < 256; i++ {
		cnt := 0
		for side := 0; side < 6; side++ {
			for rot := 0; rot < 4; rot++ {
				chord := n.Cube.Get(side, rot, i)
				cnt += chord.ActiveCount()
			}
		}
		if cnt > bestCount {
			bestCount = cnt
			bestFace = i
		}
	}
	return bestFace
}
