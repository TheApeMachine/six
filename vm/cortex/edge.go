package cortex

import "github.com/theapemachine/six/data"

// Patch identifies a specific topological docking port on a 3D Cube (6 sides x 4 rotations).
type Patch struct {
	Side int // 0 to 5
	Rot  int // 0 to 3
}

// Edge defines a connection between two geometric contact patches on adjacent nodes.
// The operation (Op) determines its behavioral band.
type Edge struct {
	A            *Node
	PatchA       Patch
	B            *Node
	PatchB       Patch
	Op           Opcode
	StableFrames int

	// The open communication channels across this synapse.
	// Computed continuously as Channel = ChordAND(A.CubeChord(), B.CubeChord())
	ChannelMask data.Chord

	// Observational metrics
	TokensSent int
}

// Refresh re-evaluates the opcode based on the current GF(257) states of the endpoints.
func (e *Edge) Refresh() {
	gateA := e.A.Cube.Face256(e.PatchA.Side, e.PatchA.Rot)
	gateB := e.B.Cube.Face256(e.PatchB.Side, e.PatchB.Rot)
	newOp := DeriveOpcode(gateA, gateB)
	if newOp != e.Op {
		e.Op = newOp
		e.StableFrames = 0
	} else {
		e.StableFrames++
	}

	aChord := e.A.CubeChord()
	bChord := e.B.CubeChord()
	e.ChannelMask = data.ChordAND(&aChord, &bChord)
}

// Weight computes the topological firing probability. Volatile edges fire more often.
func (e *Edge) Weight() float64 {
	return 1.0 / (1.0 + float64(e.StableFrames)*0.02)
}
