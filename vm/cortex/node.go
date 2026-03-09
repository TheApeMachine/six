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
It owns a 3D Cube (6 sides x 4 rotations x 257 chords) as volatile working memory.
Energy is never stored separately — it IS the total popcount density of the cube.
*/
type Node struct {
	ID      int
	Cube    geometry.Cube
	Rot     geometry.GFRotation
	Header  geometry.ManifoldHeader
	edges   []*Edge
	inbox   chan Token
	Signals []Token // observability for routed signals
	traffic int
	birth   int // tick at which this node was created

	// searchPending is set by OpSearch to signal the graph
	// to perform topological expansion from this node.
	searchPending bool

	// Cached cube chord — OR-fold of all 257 face chords.
	cubeChordCache data.Chord
	cubeChordDirty bool
}

/*
NewNode allocates a node with IdentityRotation, empty Cube, and buffered inbox.
*/
func NewNode(id, birthTick int) *Node {
	return &Node{
		ID:             id,
		Rot:            geometry.IdentityRotation(),
		inbox:          make(chan Token, defaultInboxSize),
		Signals:        make([]Token, 0),
		birth:          birthTick,
		cubeChordDirty: true,
	}
}

/*
Connect establishes a mutual topological Edge between n and other. Ignores nil, self, and duplicates.
For bidirectional topology, this maintains a single Edge instance referenced by both.
*/
func (n *Node) Connect(other *Node) {
	if other == nil || other == n {
		return
	}
	for _, e := range n.edges {
		if e.A == other || e.B == other {
			return
		}
	}

	patchA := Patch{Side: n.EdgeCount() % 6, Rot: (n.EdgeCount() / 6) % 4}
	patchB := Patch{Side: other.EdgeCount() % 6, Rot: (other.EdgeCount() / 6) % 4}

	edge := &Edge{
		A:      n,
		B:      other,
		PatchA: patchA,
		PatchB: patchB,
	}
	edge.Refresh()

	n.edges = append(n.edges, edge)
	other.edges = append(other.edges, edge)
}

/*
Edges returns the associated connected edges.
*/
func (n *Node) Edges() []*Edge { return n.edges }

/*
EdgeCount returns the number of connections.
*/
func (n *Node) EdgeCount() int { return len(n.edges) }

/*
Energy returns total Cube popcount density. Range [0, 1]. No separate counter.
*/
func (n *Node) Energy() float64 {
	total := 0
	for side := 0; side < 6; side++ {
		for rot := 0; rot < 4; rot++ {
			for i := 0; i < 256; i++ {
				chord := n.Cube.Get(side, rot, i)
				total += chord.ActiveCount()
			}
		}
	}
	// 24 patches × 256 faces × 257 logical bits
	return float64(total) / float64(24*256*257)
}

/*
FaceDensity returns sum of ActiveCount(face)/257 across all 24 patches for the given physical face.
*/
func (n *Node) FaceDensity(face int) float64 {
	total := 0
	for side := 0; side < 6; side++ {
		for rot := 0; rot < 4; rot++ {
			chord := n.Cube.Get(side, rot, face)
			total += chord.ActiveCount()
		}
	}
	return float64(total) / float64(24.0*257.0)
}

/*
Send enqueues tok for the next DrainInbox. Non-blocking; drops if inbox full.
*/
func (n *Node) Send(tok Token) {
	select {
	case n.inbox <- tok:
	default:
		// inbox saturated — token dissipates
	}
}

/*
DrainInbox removes and returns all tokens currently in the inbox.
*/
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
Result is cached and invalidated on any Cube write.
*/
func (n *Node) CubeChord() data.Chord {
	if !n.cubeChordDirty {
		return n.cubeChordCache
	}
	var summary data.Chord
	for side := 0; side < 6; side++ {
		for rot := 0; rot < 4; rot++ {
			for i := 0; i < 256; i++ {
				c := n.Cube.Get(side, rot, i)
				summary = data.ChordOR(&summary, &c)
			}
		}
	}
	n.cubeChordCache = summary
	n.cubeChordDirty = false
	return summary
}

/*
InvalidateChordCache marks the CubeChord cache stale. Call after any Cube write.
*/
func (n *Node) InvalidateChordCache() {
	n.cubeChordDirty = true
}

/*
WipeFace clears the 512-bit chord at the specified logical face index.
The physical face is determined by the current GF(257) lens.
*/
func (n *Node) WipeFace(logicalFace int) {
	if logicalFace < 0 || logicalFace >= geometry.CubeFaces {
		return
	}
	physFace := n.Rot.Forward(logicalFace)
	for side := 0; side < 6; side++ {
		for rot := 0; rot < 4; rot++ {
			n.Cube.Set(side, rot, physFace, data.Chord{})
		}
	}
	n.InvalidateChordCache()
}
