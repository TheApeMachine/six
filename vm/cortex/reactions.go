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
		if !n.rememberSignal(tok) {
			return
		}

		_ = n.invokeTool(tok)

		// 1. Memory Resonance (The Deduction Phase)
		// A signal interacts with the node's crystallized mass.
		cubeChord := n.CubeChord()
		sim := data.ChordSimilarity(&tok.Chord, &cubeChord)

		// If they share structural primes (e.g. 'Sandra'), they resonate.
		if sim > 0 {
			minResonance := max(tok.Chord.ActiveCount()/2, 1)

			if sim >= minResonance {
				reaction := data.ChordHole(&cubeChord, &tok.Chord)

				if reaction.ActiveCount() > 0 {
					reflection := NewSignalToken(reaction, reaction, n.ID)
					reflection.TTL = tok.TTL // Inherit remaining kinetic energy
					reflection.Program = n.Program

					for _, edge := range n.edges {
						neighbor := edge.A
						if neighbor == n {
							neighbor = edge.B
						}
						if neighbor.ID != tok.Origin {
							neighbor.Send(reflection)
						}
					}
				}
			}
		}

		if tok.TTL <= 1 {
			return // signal dies
		}

		// 2. Wave Propagation
		// Forward the original query signal along open channels.
		for _, edge := range n.edges {
			neighbor := edge.A
			if neighbor == n {
				neighbor = edge.B
			}
			if neighbor.ID == tok.Origin {
				continue
			}

			// Diffuse along compatible channels or open unstructured space
			overlap := data.ChordAND(&tok.SignalMask, &edge.ChannelMask)
			controlOpen := false
			if tok.SignalMask.ActiveCount() == 1 && tok.SignalMask.Has(256) {
				controlOpen = edge.ControlMask.ActiveCount() > 0
			}

			if overlap.ActiveCount() > 0 || controlOpen || edge.ChannelMask.ActiveCount() == 0 {
				forward := tok
				forward.TTL--
				forward.Origin = n.ID
				neighbor.Send(forward)
			}
		}
		return
	}

	_ = n.invokeTool(tok)

	switch tok.Op {
	case OpRotateX:
		n.Rot = n.Rot.Compose(geometry.DefaultRotTable.X90)
		n.Rot = n.Rot.Compose(tok.Carry)
	case OpRotateY:
		n.Rot = n.Rot.Compose(geometry.DefaultRotTable.Y90)
		n.Rot = n.Rot.Compose(tok.Carry)
	case OpRotateZ:
		n.Rot = n.Rot.Compose(geometry.DefaultRotTable.Z90)
		n.Rot = n.Rot.Compose(tok.Carry)

	case OpAlign, OpCompose:
		n.Rot = n.Rot.Compose(tok.Carry)

	case OpFork:
		n.Rot = n.Rot.Compose(tok.Carry)
		n.noteFork(tok.Chord, tok.Program)

	case OpSync:
		a := max((int(n.Rot.A)+int(tok.Carry.A))/2, 1)
		n.Rot.A = uint16(a)
		n.Rot.B = uint16((int(n.Rot.B) + int(tok.Carry.B)) / 2 % 257)

	case OpSearch:
		n.Rot = n.Rot.Compose(tok.Carry)
		n.searchPending = true
		if tok.Chord.ActiveCount() > 0 && n.Interface.ActiveCount() == 0 {
			n.Interface = tok.Chord
		}
	}

	carrier := tok.Program
	stateCarrier := tok.Carry.StateChord()

	if stateCarrier.ActiveCount() > 0 {
		carrier = data.ChordOR(&carrier, &stateCarrier)
	}

	if tok.Program.ActiveCount() > 0 {
		n.Program = data.ChordOR(&n.Program, &tok.Program)
		n.Cube.InjectControl(tok.Program, carrier, n.Rot)
	}

	// Token data is absorbed directly into the Cube guided by the node's
	// GF(257) lens and routed into a sparse subset of side-rotation lanes.
	n.Cube.Inject(tok.LogicalFace, tok.Chord, carrier, n.Rot)
	n.InvalidateChordCache()

	anchor, hole, _, shouldDream := n.Hole()
	if shouldDream {
		dreamMask := anchor
		if dreamMask.ActiveCount() == 0 {
			dreamMask = hole
		}

		n.Cube.InjectControl(hole, dreamMask, n.Rot)
		n.InvalidateChordCache()

		dream := NewSignalToken(hole, dreamMask, n.ID)
		dream.TTL = max(tok.TTL, defaultTTL)
		dream.Program = n.Program

		for _, edge := range n.edges {
			neighbor := edge.A
			if neighbor == n {
				neighbor = edge.B
			}

			if neighbor.ID != tok.Origin {
				neighbor.Send(dream)
			}
		}
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

	for _, edge := range n.edges {
		neighbor := edge.A
		if neighbor == n {
			neighbor = edge.B
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

	if bestAnchorSize == 0 {
		for side := 0; side < 6; side++ {
			for rot := 0; rot < 4; rot++ {
				faceChord := n.Cube.Get(side, rot, bestFaceIdx)
				bestAnchor = data.ChordOR(&bestAnchor, &faceChord)
			}
		}

		bestResidue = data.ChordHole(&summary, &bestAnchor)
		bestAnchorSize = bestAnchor.ActiveCount()
	}

	return bestAnchor, bestResidue, bestFaceIdx, bestAnchorSize >= 3 && bestResidue.ActiveCount() > 0
}

/*
bestPhysicalFace returns the physical face index (0-257) with highest aggregate ActiveCount.
*/
func (n *Node) bestPhysicalFace() int {
	bestFace := 256
	bestCount := 0
	for face := 0; face < 256; face++ {
		count := 0
		for side := 0; side < 6; side++ {
			for rot := 0; rot < 4; rot++ {
				chord := n.Cube.Get(side, rot, face)
				count += chord.ActiveCount()
			}
		}
		if count > bestCount {
			bestCount = count
			bestFace = face
		}
	}
	return bestFace
}
