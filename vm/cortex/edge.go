package cortex

import "github.com/theapemachine/six/data"

/*
Patch identifies a specific topological docking port
on a 3D Cube (6 sides x 4 rotations).
*/
type Patch struct {
	Side int // 0 to 5
	Rot  int // 0 to 3
}

/*
Edge defines a connection between two geometric contact patches
on adjacent nodes. The operation (Op) determines its behavioral band.
*/
type Edge struct {
	A            *Node
	PatchA       Patch
	B            *Node
	PatchB       Patch
	Op           Opcode
	StableFrames int

	// The open communication channels across this synapse.
	// Computed as Channel = ChordAND(A.CubeChord(), B.CubeChord())
	ChannelMask data.Chord

	// Observational metrics
	TokensSent int
}

/*
Refresh re-evaluates the opcode based on the current
GF(257) states of the endpoints.
*/
func (edge *Edge) Refresh() {
	gateA := edge.A.Cube.Face256(edge.PatchA.Side, edge.PatchA.Rot)
	gateB := edge.B.Cube.Face256(edge.PatchB.Side, edge.PatchB.Rot)
	newOp := DeriveOpcode(gateA, gateB)

	edge.StableFrames++

	if newOp != edge.Op {
		edge.Op = newOp
		edge.StableFrames = 0
	}

	aChord := edge.A.CubeChord()
	bChord := edge.B.CubeChord()
	edge.ChannelMask = data.ChordAND(&aChord, &bChord)
}

/*
Weight computes the topological firing probability.
Volatile edges fire more often.
*/
func (edge *Edge) Weight() float64 {
	return 1.0 / (1.0 + float64(edge.StableFrames)*0.02)
}
