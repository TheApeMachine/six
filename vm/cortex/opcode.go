package cortex

import "github.com/theapemachine/six/geometry"

/*
Opcode defines the GF(257) topological operation to perform.
*/
type Opcode byte

const (
	OpRotateX Opcode = iota // Volatile: physical cascade
	OpRotateY
	OpRotateZ
	OpAlign // Stable: structural convergence
	OpSearch
	OpSync
	OpFork // Growth: topological expansion
	OpCompose
)

/*
Band returns the classification of the opcode (rotate, stable, growth).
*/
func (opcode Opcode) Band() string {
	switch opcode {
	case OpRotateX, OpRotateY, OpRotateZ:
		return "rotate"
	case OpAlign, OpSearch, OpSync:
		return "stable"
	case OpFork, OpCompose:
		return "growth"
	default:
		return "stable"
	}
}

/*
DeriveOpcode computes the operation geometrically dictated by two connection endpoints.
Band boundaries: rotate ~43%, stable ~45%, growth ~12%.
Tuned to prevent growth-dominated freeze.
*/
func DeriveOpcode(stateA, stateB geometry.GFRotation) Opcode {
	faceA := stateA.Reverse(256)
	faceB := stateB.Reverse(256)

	val := (faceA + faceB) % 257

	switch {
	case val < 40:
		return OpRotateX
	case val < 80:
		return OpRotateY
	case val < 110:
		return OpRotateZ
	case val < 150:
		return OpAlign
	case val < 190:
		return OpSearch
	case val < 225:
		return OpSync
	case val < 245:
		return OpFork
	default:
		return OpCompose
	}
}
