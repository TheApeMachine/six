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
	ID     int
	Cube   geometry.Cube
	Rot    geometry.GFRotation
	Header geometry.ManifoldHeader
	Role   NodeRole
	// Interface is the chord this node binds to the graph with.
	// For registers it is the unresolved subproblem; for tools it is the
	// invocation signature.
	Interface data.Chord
	// Payload is the contribution this node offers back to the graph.
	// For registers it is the focused residue; for tools it is the reusable
	// response pattern.
	Payload data.Chord
	// Program is the local control-plane seed exposed on face 256.
	Program data.Chord
	Support int
	edges   []*Edge
	inbox   chan Token
	Signals []Token // observability for routed signals
	traffic int
	birth   int // tick at which this node was created

	// searchPending is set by OpSearch to signal the graph
	// to perform topological expansion from this node.
	searchPending bool
	forkPending   bool

	// Cached cube chord — OR-fold of all 257 face chords.
	cubeChordCache data.Chord
	cubeChordDirty bool

	signalIndex   map[signalKey]int
	signalTTL     map[signalKey]int
	signalSupport map[signalKey]int
}

/*
NewNode allocates a node with IdentityRotation, empty Cube, and buffered inbox.
*/
func NewNode(id, birthTick int) *Node {
	return &Node{
		ID:             id,
		Rot:            geometry.IdentityRotation(),
		Role:           RoleCore,
		inbox:          make(chan Token, defaultInboxSize),
		Signals:        make([]Token, 0),
		birth:          birthTick,
		cubeChordDirty: true,
		signalIndex:    make(map[signalKey]int),
		signalTTL:      make(map[signalKey]int),
		signalSupport:  make(map[signalKey]int),
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
Energy returns total Cube popcount density. Range [0, 1]. No separate counter.
*/
func (n *Node) Energy() float64 {
	total := 0
	for side := 0; side < 6; side++ {
		for rot := 0; rot < 4; rot++ {
			for face := 0; face < 256; face++ {
				chord := n.Cube.Get(side, rot, face)
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
func (node *Node) BestFace() int {
	bestFace := 256 // default to delimiter (stop)
	bestCount := 0

	for face := 0; face < 256; face++ {
		cnt := 0

		for side := 0; side < 6; side++ {
			for rot := 0; rot < 4; rot++ {
				chord := node.Cube.Get(side, rot, face)
				cnt += chord.ActiveCount()
			}
		}

		if cnt > bestCount {
			bestCount = cnt
			bestFace = face
		}
	}

	// Reverse the rotation to recover the logical byte value.
	// Physical face → Rot.Reverse → logical byte.
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
	if !node.cubeChordDirty {
		return node.cubeChordCache
	}

	var summary data.Chord

	for side := 0; side < 6; side++ {
		for rot := 0; rot < 4; rot++ {
			for face := 0; face < 256; face++ {
				chord := node.Cube.Get(side, rot, face)
				summary = data.ChordOR(&summary, &chord)
			}
		}
	}

	node.cubeChordCache = summary
	node.cubeChordDirty = false

	return summary
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

	for side := 0; side < 6; side++ {
		for rot := 0; rot < 4; rot++ {
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
	node.Role = RoleCore
	node.Interface = data.Chord{}
	node.Payload = data.Chord{}
	node.Program = data.Chord{}
	node.Support = 0
	node.edges = nil
	node.Signals = node.Signals[:0]
	node.traffic = 0
	node.birth = birthTick
	node.searchPending = false
	node.forkPending = false

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
ResetForPrompt clears prompt-local activity while optionally preserving a
compiled tool node across prompt cycles.
*/
func (node *Node) ResetForPrompt(birthTick int, preserveTool bool) {
	role := node.Role
	interfaceChord := node.Interface
	payload := node.Payload
	program := node.Program
	support := node.Support

	node.Reset(birthTick)

	if preserveTool && role == RoleTool {
		node.Role = RoleTool
		node.Interface = interfaceChord
		node.Payload = payload
		node.Program = program
		node.Support = support

		if program.ActiveCount() > 0 {
			node.Rot = geometry.RotationForChord(program)
		}

		node.seedSpecialization()
	}
}

/*
SearchChord returns the residue that still needs to be resolved. When no
explicit hole is present it falls back to the node's full summary chord.
*/
func (node *Node) SearchChord() data.Chord {
	if node.Role == RoleRegister && node.Payload.ActiveCount() > 0 {
		return node.Payload
	}

	if node.Role == RoleTool && node.Interface.ActiveCount() > 0 {
		return node.Interface
	}

	_, hole, _, shouldDream := node.Hole()

	if shouldDream && hole.ActiveCount() > 0 {
		return hole
	}

	return node.CubeChord()
}
