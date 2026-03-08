package cortex

import (
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

const (
	// defaultInboxSize is the channel buffer depth per node.
	defaultInboxSize = 256

	// defaultTTL controls how many hops a token can survive.
	defaultTTL = 8
)

/*
Node is a single reactive unit in the cortex graph.
It owns a MacroCube (257 faces × 512 bits = 16 KB) as volatile working memory,
a GF(257) rotation state as its geometric "lens", and an inbox channel for
receiving Chord tokens from neighbors.

Energy is never stored separately — it IS the total popcount density of the cube.
A dense cube is energetic. A sparse cube is starved.
*/
type Node struct {
	ID      int
	Cube    geometry.MacroCube
	Rot     geometry.GFRotation
	Header  geometry.ManifoldHeader
	edges   []*Node
	inbox   chan Token
	traffic int
	birth   int // tick at which this node was created
}

// NewNode creates a fresh node with an identity rotation lens and an empty cube.
func NewNode(id, birthTick int) *Node {
	return &Node{
		ID:    id,
		Rot:   geometry.IdentityRotation(),
		inbox: make(chan Token, defaultInboxSize),
		birth: birthTick,
	}
}

// Connect establishes a directed edge from this node to `other`.
// Duplicate and self-edges are silently ignored.
func (n *Node) Connect(other *Node) {
	if other == nil || other == n {
		return
	}
	for _, e := range n.edges {
		if e == other {
			return
		}
	}
	n.edges = append(n.edges, other)
}

// Edges returns the current neighbor list. Read-only view.
func (n *Node) Edges() []*Node { return n.edges }

// EdgeCount returns the number of outgoing edges.
func (n *Node) EdgeCount() int { return len(n.edges) }

/*
Energy derives the thermodynamic state of the node from the total popcount
density of its MacroCube. This is the ONLY energy accounting — no separate
counter. A fully saturated cube returns 1.0, an empty cube returns 0.0.
*/
func (n *Node) Energy() float64 {
	total := 0
	for i := range geometry.CubeFaces {
		total += n.Cube[i].ActiveCount()
	}
	// 257 faces × 257 logical bits (max popcount per chord in the 257-bit invariant)
	return float64(total) / float64(geometry.CubeFaces*257)
}

// FaceDensity returns the fractional occupancy of a single face (0.0 – 1.0).
func (n *Node) FaceDensity(face int) float64 {
	return float64(n.Cube[face].ActiveCount()) / 257.0
}

// Send enqueues a token for processing on the next tick.
// Non-blocking: if the inbox is full the token is silently dropped.
func (n *Node) Send(tok Token) {
	select {
	case n.inbox <- tok:
	default:
		// inbox saturated — token dissipates
	}
}

// DrainInbox collects all pending tokens for processing.
func (n *Node) DrainInbox() []Token {
	var batch []Token
	for {
		select {
		case tok := <-n.inbox:
			batch = append(batch, tok)
		default:
			return batch
		}
	}
}

/*
BestFace scans all 257 faces of the cube and returns the LOGICAL face index
(the byte value) with the highest popcount. The physical face is mapped back
through the node's GFRotation inverse to recover the self-addressed byte value.

If no face is active, returns 256 (delimiter = stop signal).
*/
func (n *Node) BestFace() int {
	bestFace := 256 // default to delimiter (stop)
	bestCount := 0
	for i := range geometry.CubeFaces {
		cnt := n.Cube[i].ActiveCount()
		if cnt > bestCount {
			bestCount = cnt
			bestFace = i
		}
	}
	// Reverse the rotation to recover the logical byte value.
	// Physical face → Rot.Reverse → logical byte.
	if bestFace < 256 {
		bestFace = n.Rot.Reverse(bestFace)
	}
	return bestFace
}

/*
CubeChord compresses the entire MacroCube into a single summary chord
by OR-folding all 257 face chords. This is the node's "signature".
*/
func (n *Node) CubeChord() data.Chord {
	var summary data.Chord
	for i := range geometry.CubeFaces {
		summary = data.ChordOR(&summary, &n.Cube[i])
	}
	return summary
}
