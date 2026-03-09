package cortex

import "github.com/theapemachine/six/data"

// Edge defines a connection between two nodes in the geometric hyper-graph.
// The operation (Op) determines its behavioral band.
type Edge struct {
	A            *Node
	B            *Node
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
	newOp := DeriveOpcode(e.A.Rot, e.B.Rot)
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
