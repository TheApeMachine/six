package cortex

import (
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

/*
Patch identifies a specific topological docking port
on a 3D Cube (6 sides x 4 rotations).
*/
type Patch struct {
	Side int // 0 to 5
	Rot  int // 0 to 3
}

/*
Edge defines a connection between two geometric contact patches on adjacent
nodes. The edge itself is the executable logic surface: its opcode is derived
from the face-256 control gates plus the currently exposed free chords.
*/
type Edge struct {
	A            *Node
	PatchA       Patch
	B            *Node
	PatchB       Patch
	Op           Opcode
	StableFrames int

	ChannelMask data.Chord
	ControlMask data.Chord
	Resonance   data.Chord
	Program     data.Chord
	GateA       data.Chord
	GateB       data.Chord
	FreeA       data.Chord
	FreeB       data.Chord
	PressureA   int
	PressureB   int
	ComposeHits int
	Activation  float64

	// Observational metrics
	TokensSent int
}

func endpointGate(node *Node, patch Patch) data.Chord {
	gate := node.Cube.Face256(patch.Side, patch.Rot)
	if gate.ActiveCount() == 0 {
		gate = node.Program
	}
	if gate.ActiveCount() == 0 {
		gate = node.Payload
	}
	return gate
}

func endpointFree(node *Node, patch Patch) data.Chord {
	freeFace := node.Rot.Reverse(256)
	free := node.Cube.Get(patch.Side, patch.Rot, freeFace)

	if free.ActiveCount() == 0 && node.Payload.ActiveCount() > 0 {
		free = node.Payload
	}
	if free.ActiveCount() == 0 && node.Interface.ActiveCount() > 0 {
		free = node.Interface
	}
	if free.ActiveCount() == 0 {
		summary := node.CubeChord()
		face := chordFace(summary)
		free = node.Cube.Get(patch.Side, patch.Rot, face)
		if free.ActiveCount() == 0 {
			free = summary
		}
	}

	return free
}

/*
Refresh re-evaluates the edge-local control program based on the exposed
GF(257) states of both endpoints.
*/
func (edge *Edge) Refresh() {
	gateA := endpointGate(edge.A, edge.PatchA)
	gateB := endpointGate(edge.B, edge.PatchB)
	freeA := endpointFree(edge.A, edge.PatchA)
	freeB := endpointFree(edge.B, edge.PatchB)
	newOp := DeriveEdgeOpcode(gateA, freeA, gateB, freeB)

	if edge.A.Role == RoleTool && edge.B.Role == RoleTool {
		sharedTools := data.ChordAND(&freeA, &freeB)
		if sharedTools.ActiveCount() > 0 {
			newOp = OpCompose
		}
	} else if edge.A.Role == RoleTool && resonate(freeB, edge.A.Interface) {
		newOp = OpCompose
	} else if edge.B.Role == RoleTool && resonate(freeA, edge.B.Interface) {
		newOp = OpCompose
	}

	edge.StableFrames++
	if newOp != edge.Op {
		edge.Op = newOp
		edge.StableFrames = 0
	}

	resonance := data.ChordAND(&freeA, &freeB)
	control := data.ChordAND(&gateA, &gateB)
	residueA := data.ChordHole(&freeA, &freeB)
	residueB := data.ChordHole(&freeB, &freeA)
	program := data.ChordOR(&residueA, &residueB)
	program = data.ChordOR(&program, &control)
	if program.ActiveCount() == 0 {
		program = data.ChordOR(&gateA, &gateB)
	}

	edge.GateA = gateA
	edge.GateB = gateB
	edge.FreeA = freeA
	edge.FreeB = freeB
	edge.Resonance = resonance
	edge.ChannelMask = resonance
	edge.ControlMask = control
	edge.Program = program
	edge.PressureA = residueA.ActiveCount() + gateA.ActiveCount()
	edge.PressureB = residueB.ActiveCount() + gateB.ActiveCount()

	if edge.Op == OpCompose && edge.A.Role == RoleTool && edge.B.Role == RoleTool && resonance.ActiveCount() > 0 {
		edge.ComposeHits++
	} else if edge.Op != OpCompose {
		edge.ComposeHits = 0
	}

	edge.Activation = edge.ActivationWeight()
}

func (edge *Edge) chooseDirection() (*Node, *Node, data.Chord, data.Chord) {
	from := edge.A
	to := edge.B
	fromFree := edge.FreeA
	toFree := edge.FreeB

	if edge.Op == OpCompose {
		if edge.A.Role == RoleTool && edge.A.matchesInterface(edge.FreeB) {
			return edge.B, edge.A, edge.FreeB, edge.FreeA
		}
		if edge.B.Role == RoleTool && edge.B.matchesInterface(edge.FreeA) {
			return edge.A, edge.B, edge.FreeA, edge.FreeB
		}
	}

	switch {
	case edge.PressureB > edge.PressureA:
		from, to = edge.B, edge.A
		fromFree, toFree = edge.FreeB, edge.FreeA
	case edge.PressureA == edge.PressureB && edge.B.ID < edge.A.ID:
		from, to = edge.B, edge.A
		fromFree, toFree = edge.FreeB, edge.FreeA
	}

	return from, to, fromFree, toFree
}

func logicalFaceForOp(op Opcode, chord data.Chord) int {
	switch op {
	case OpRotateX, OpRotateY, OpRotateZ, OpSearch, OpFork, OpCompose:
		return 256
	default:
		return chordFace(chord)
	}
}

/*
Pulse synthesizes the token emitted by this edge for the current tick.
The token is local to the edge program rather than the sender's entire cube.
*/
func (edge *Edge) Pulse() (*Node, *Node, Token, bool) {
	from, to, fromFree, toFree := edge.chooseDirection()
	residue := data.ChordHole(&fromFree, &toFree)
	programRot := geometry.IdentityRotation()
	if edge.Program.ActiveCount() > 0 {
		programRot = geometry.RotationForChord(edge.Program)
	}

	payload := edge.Resonance
	switch edge.Op {
	case OpRotateX, OpRotateY, OpRotateZ:
		payload = residue
		if payload.ActiveCount() == 0 {
			payload = edge.Program
		}
	case OpAlign:
		payload = edge.Resonance
		if payload.ActiveCount() == 0 {
			payload = edge.ChannelMask
		}
	case OpSync:
		payload = data.ChordOR(&edge.Resonance, &edge.ControlMask)
	case OpSearch:
		payload = residue
		if payload.ActiveCount() == 0 {
			payload = edge.Program
		}
	case OpFork:
		payload = residue
		if payload.ActiveCount() == 0 {
			payload = fromFree
		}
	case OpCompose:
		payload = data.ChordOR(&edge.Resonance, &edge.Program)
		if payload.ActiveCount() == 0 {
			payload = fromFree
		}
	}

	if payload.ActiveCount() == 0 {
		return nil, nil, Token{}, false
	}

	tok := Token{
		Chord:       payload,
		LogicalFace: logicalFaceForOp(edge.Op, payload),
		Origin:      from.ID,
		TTL:         1,
		Op:          edge.Op,
		Carry:       programRot,
		Program:     edge.Program,
	}

	return from, to, tok, true
}

/*
ActivationWeight computes how strongly the edge should participate in the
current tick. Hot, high-tension edges fire more readily than stale ones.
*/
func (edge *Edge) ActivationWeight() float64 {
	base := 0.01
	switch edge.Op {
	case OpRotateX, OpRotateY, OpRotateZ:
		base = 0.08
	case OpFork:
		base = 0.06
	case OpCompose:
		base = 0.05
	case OpSync:
		base = 0.04
	case OpAlign:
		base = 0.03
	case OpSearch:
		base = 0.025
	}

	tension := edge.PressureA + edge.PressureB + edge.Program.ActiveCount() + edge.Resonance.ActiveCount()
	if tension > 64 {
		tension = 64
	}

	stability := 1.0 / (1.0 + float64(edge.StableFrames)*0.05)
	return base + float64(tension)/128.0 + stability*0.15
}

/*
Weight is kept for compatibility; it now mirrors the edge activation weight.
*/
func (edge *Edge) Weight() float64 {
	return edge.ActivationWeight()
}
