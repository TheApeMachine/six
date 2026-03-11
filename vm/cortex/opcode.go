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
	case OpAlign, OpSearch, OpSync, OpCompose:
		return "stable"
	case OpFork:
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

	// 5. Total conflict -> derive the rotation axis from the conflict residue itself.
	return deriveConflictRotation(gateA, gateB)
}

/*
DeriveEdgeOpcode resolves the executable edge operation from both the protected
face-256 control gates and the currently exposed free chords on either side of
the edge. This makes the graph itself a logic fabric: rotations can change what
an edge sees without physically copying cube state.
*/
func DeriveEdgeOpcode(gateA, freeA, gateB, freeB data.Chord) Opcode {
	freeCountA := freeA.ActiveCount()
	freeCountB := freeB.ActiveCount()
	gateCountA := gateA.ActiveCount()
	gateCountB := gateB.ActiveCount()

	if freeCountA == 0 && freeCountB == 0 {
		switch {
		case gateCountA == 0 && gateCountB == 0:
			return OpSearch
		case gateCountA == 0 || gateCountB == 0:
			return OpFork
		default:
			return DeriveOpcode(gateA, gateB)
		}
	}

	if freeCountA == 0 || freeCountB == 0 {
		return OpFork
	}

	sharedFree := data.ChordAND(&freeA, &freeB)
	sharedGate := data.ChordAND(&gateA, &gateB)
	freeResidueA := data.ChordHole(&freeA, &freeB)
	freeResidueB := data.ChordHole(&freeB, &freeA)
	divergence := freeResidueA.ActiveCount() + freeResidueB.ActiveCount()

	gateResidueA := data.ChordHole(&gateA, &gateB)
	gateResidueB := data.ChordHole(&gateB, &gateA)

	if sharedFree.ActiveCount() > 0 && divergence == 0 && gateResidueA.ActiveCount() == 0 && gateResidueB.ActiveCount() == 0 {
		return OpSync
	}

	if sharedFree.ActiveCount() > 0 && sharedGate.ActiveCount() > 0 {
		return OpCompose
	}

	if sharedFree.ActiveCount() > 0 {
		return OpAlign
	}

	if sharedGate.ActiveCount() > 0 {
		return OpCompose
	}

	effectiveA := data.ChordOR(&gateA, &freeA)
	effectiveB := data.ChordOR(&gateB, &freeB)

	return deriveConflictRotation(effectiveA, effectiveB)
}

/*
deriveConflictRotation selects one of the three physical rotation bands from
the residue left by two incompatible gates. This prevents every conflict from
collapsing onto RotateX and unlocks genuinely multi-axis cortex dynamics.
*/
func deriveConflictRotation(gateA, gateB data.Chord) Opcode {
	residueA := data.ChordHole(&gateA, &gateB)
	residueB := data.ChordHole(&gateB, &gateA)
	conflict := data.ChordOR(&residueA, &residueB)

	switch data.ChordBin(&conflict) % 3 {
	case 0:
		return OpRotateX
	case 1:
		return OpRotateY
	default:
		return OpRotateZ
	}
}
