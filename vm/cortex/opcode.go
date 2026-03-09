package cortex

import "github.com/theapemachine/six/data"

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
It uses purely bitwise operations (ActiveCount, ChordHole, ChordSimilarity) on the
257th chords (Geometric Gates) mapped to the connected topological patches.
*/
func DeriveOpcode(gateA, gateB data.Chord) Opcode {
	countA := gateA.ActiveCount()
	countB := gateB.ActiveCount()

	// 1. Both gates are totally empty (void) - search wavefront
	if countA == 0 && countB == 0 {
		return OpSearch
	}

	// 2. Growth (information transfer from source to void)
	if (countA == 0 && countB > 0) || (countA > 0 && countB == 0) {
		return OpFork
	}

	// 3. Sync (Geometric Resolution) -> When Gate A and Gate B share exact shapes or exact holes
	sim := data.ChordSimilarity(&gateA, &gateB)
	if sim > 0 && sim == countA && sim == countB {
		return OpSync
	}

	// 4. Align -> Partial similarities pulling them together
	if sim > 0 {
		return OpAlign
	}

	// 5. Total conflict (no shared properties) -> Forces Rotate
	return OpRotateX // Systematically dodging the conflict
}
