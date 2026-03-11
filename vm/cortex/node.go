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
	ID       int
	Cube     geometry.Cube
	Rot      geometry.GFRotation
	Header   geometry.ManifoldHeader
	edges    []*Edge
	inbox    chan Token
	Signals  []Token // observability for routed signals
	drainBuf []Token // reused buffer for DrainInbox
	traffic  int
	birth    int // tick at which this node was created

	// searchPending is set by OpSearch to signal the graph
	// to perform topological expansion from this node.
	searchPending bool

	// Cached cube chord — OR-fold of all 256 face chords.
	// bestFaceIdxCache and facePopcount are computed in the same fused pass.
	cubeChordCache     data.Chord
	bestFaceIdxCache   int
	bestFaceCountCache int
	facePopcount       [257]int
	totalPopcount      int
	cubeChordDirty     bool

	signalIndex   map[signalKey]int
	signalTTL     map[signalKey]int
	signalSupport map[signalKey]int
}

/*
NewNode allocates a node with IdentityRotation, empty Cube, and buffered inbox.
*/
func NewNode(id, birthTick int) *Node {
	return &Node{
		ID:               id,
		Rot:              geometry.IdentityRotation(),
		inbox:            make(chan Token, defaultInboxSize),
		Signals:          make([]Token, 0),
		birth:            birthTick,
		cubeChordDirty:   true,
		bestFaceIdxCache: 256,
		signalIndex:      make(map[signalKey]int),
		signalTTL:        make(map[signalKey]int),
		signalSupport:    make(map[signalKey]int),
	}
}

/*
Connect establishes a mutual topological Edge between n and other.
Ignores nil, self, and duplicates. For bidirectional topology,
this maintains a single Edge instance referenced by both.
*/
func (n *Node) Connect(other *Node) {
	if other == nil || other == n {
		return
	}
	for _, edge := range n.edges {
		if edge.A == other || edge.B == other {
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
Energy returns total Cube popcount density.
Range [0, 1]. No separate counter.
*/
func (n *Node) Energy() float64 {
	if n.cubeChordDirty {
		n.recomputeCubeStats()
	}

	return float64(n.totalPopcount) / float64(24*256*257)
}

/*
FaceDensity returns sum of ActiveCount(face)/257 across all
24 patches for the given physical face.
*/
func (n *Node) FaceDensity(face int) float64 {
	if n.cubeChordDirty {
		n.recomputeCubeStats()
	}

	return float64(n.facePopcount[face]) / float64(24.0*257.0)
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
Reuses a pre-allocated buffer to avoid per-drain allocations.
*/
func (n *Node) DrainInbox() []Token {
	n.drainBuf = n.drainBuf[:0]

	for {
		select {
		case tok := <-n.inbox:
			n.drainBuf = append(n.drainBuf, tok)
		default:
			if len(n.drainBuf) == 0 {
				return nil
			}
			out := make([]Token, len(n.drainBuf))
			copy(out, n.drainBuf)
			return out
		}
	}
}

/*
BestFace returns the LOGICAL face index (the byte value) with the highest
aggregate popcount across all 24 patches.  Delegates to recomputeCubeStats
for the physical face, then maps through the GFRotation inverse.

If no face is active, returns 256 (delimiter = stop signal).
*/
func (node *Node) BestFace() int {
	if node.cubeChordDirty {
		node.recomputeCubeStats()
	}

	bestFace := node.bestFaceIdxCache

	if bestFace < 256 {
		bestFace = node.Rot.Reverse(bestFace)
	}

	return bestFace
}

/*
CubeChord compresses the entire MacroCube into a single summary chord
by OR-folding all 257 face chords. This is the node's "signature".
Result is cached and invalidated on any Cube write.
*/
func (node *Node) CubeChord() data.Chord {
	if node.cubeChordDirty {
		node.recomputeCubeStats()
	}

	return node.cubeChordCache
}

/*
recomputeCubeStats performs a single fused pass over all 6×4×256 cube slots,
computing the OR-fold summary chord, per-face popcount, total popcount, and
the densest physical face index.  Called lazily when cubeChordDirty is true
(after wipe/reset cold paths).  Hot-path updates go through absorbFace.
*/
func (node *Node) recomputeCubeStats() {
	var summary data.Chord
	bestFace := 256
	bestCount := 0
	totalPop := 0

	for face := range 256 {
		faceCount := 0

		for side := range 6 {
			for rot := range 4 {
				chord := &node.Cube.Sides[side][rot][face]

				summary[0] |= chord[0]
				summary[1] |= chord[1]
				summary[2] |= chord[2]
				summary[3] |= chord[3]
				summary[4] |= chord[4]
				summary[5] |= chord[5]
				summary[6] |= chord[6]
				summary[7] |= chord[7]

				faceCount += chord.ActiveCount()
			}
		}

		node.facePopcount[face] = faceCount
		totalPop += faceCount

		if faceCount > bestCount {
			bestCount = faceCount
			bestFace = face
		}
	}

	summary.Sanitize()
	node.cubeChordCache = summary
	node.bestFaceIdxCache = bestFace
	node.bestFaceCountCache = bestCount
	node.totalPopcount = totalPop
	node.cubeChordDirty = false
}

/*
absorbFace incrementally updates the cached cube statistics after an OR-only
write to a single physical face.  Cost is O(24) per call (one side×rot pass
over the modified face) vs O(6144) for a full recomputeCubeStats.

Safety: only valid when the cube write was an OR (bits added, never cleared).
For destructive writes (Wipe, WipeFace) use InvalidateChordCache instead.
*/
func (node *Node) absorbFace(face int, incoming *data.Chord) {
	if node.cubeChordDirty {
		node.recomputeCubeStats()
	}

	// Summary chord: OR is monotone, so absorbing the incoming bits
	// into the cached summary is equivalent to a full recompute.
	node.cubeChordCache[0] |= incoming[0]
	node.cubeChordCache[1] |= incoming[1]
	node.cubeChordCache[2] |= incoming[2]
	node.cubeChordCache[3] |= incoming[3]
	node.cubeChordCache[4] |= incoming[4]
	node.cubeChordCache[5] |= incoming[5]
	node.cubeChordCache[6] |= incoming[6]
	node.cubeChordCache[7] |= incoming[7]

	// Recompute only this face's aggregate popcount (24 iterations).
	newCount := 0

	for side := range 6 {
		for rot := range 4 {
			chord := &node.Cube.Sides[side][rot][face]
			newCount += chord.ActiveCount()
		}
	}

	oldCount := node.facePopcount[face]
	node.facePopcount[face] = newCount
	node.totalPopcount += newCount - oldCount

	if newCount > node.bestFaceCountCache {
		node.bestFaceCountCache = newCount
		node.bestFaceIdxCache = face
	}
}

/*
InvalidateChordCache marks the CubeChord cache stale. Call after any Cube write.
*/
func (node *Node) InvalidateChordCache() {
	node.cubeChordDirty = true
}

/*
WipeFace clears the 512-bit chord at the specified logical face index.
The physical face is determined by the current GF(257) lens.
*/
func (node *Node) WipeFace(logicalFace int) {
	if logicalFace < 0 || logicalFace >= geometry.CubeFaces {
		return
	}

	physFace := node.Rot.Forward(logicalFace)

	for side := range 6 {
		for rot := range 4 {
			node.Cube.Set(side, rot, physFace, data.Chord{})
		}
	}

	node.InvalidateChordCache()
}

/*
Reset clears the node's volatile state for a new prompt cycle while reusing
the allocated inbox and cube storage.
*/
func (node *Node) Reset(birthTick int) {
	node.Wipe()
	node.Rot = geometry.IdentityRotation()
	node.Header = 0
	node.edges = nil
	node.Signals = node.Signals[:0]
	node.traffic = 0
	node.birth = birthTick
	node.searchPending = false

	for {
		select {
		case <-node.inbox:
		default:
			goto cleared
		}
	}

cleared:
	clear(node.signalIndex)
	clear(node.signalTTL)
	clear(node.signalSupport)
}

/*
SearchChord returns the residue that still needs to be resolved. When no
explicit hole is present it falls back to the node's full summary chord.
*/
func (node *Node) SearchChord() data.Chord {
	_, hole, _, shouldDream := node.Hole()

	if shouldDream && hole.ActiveCount() > 0 {
		return hole
	}

	return node.CubeChord()
}
