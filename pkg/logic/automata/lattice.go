package automata

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
	config "github.com/theapemachine/six/pkg/system/core"
)

/*
Lattice is the cellular automata update rule engine. For each cell-neighbor
pair it computes the geometric AffineKey from their XOR delta and queries
the MacroIndex. Hardened opcodes are applied deterministically as affine
transforms in GF(8191); unseen deltas are recorded as candidates for
future hardening.
*/
type Lattice struct {
	opcodes *macro.MacroIndexServer
}

/*
latticeOpts configures a Lattice with options.
*/
type latticeOpts func(*Lattice)

/*
NewLattice instantiates a CA update engine backed by the shared MacroIndex.
*/
func NewLattice(opts ...latticeOpts) *Lattice {
	lattice := &Lattice{}

	for _, opt := range opts {
		opt(lattice)
	}

	return lattice
}

/*
Update applies CA rules to a cell given its neighborhood. For each neighbor
the geometric AffineKey is computed from the XOR delta. If the index contains
a hardened opcode for that key, the opcode's affine transform advances the
cell's current phase. Otherwise the delta is recorded as a candidate.
Returns the updated cell and whether any hardened transform fired.
*/
func (lattice *Lattice) Update(
	cell primitive.Value,
	neighbors []primitive.Value,
) (primitive.Value, bool) {
	changed := false

	for _, neighbor := range neighbors {
		key := macro.AffineKeyFromValues(cell, neighbor)
		opcode, found := lattice.opcodes.FindOpcode(key)

		if !found || !opcode.Hardened {
			lattice.opcodes.RecordOpcode(key)
			continue
		}

		currentPhase := numeric.Phase(cell.ResidualCarry())
		nextPhase := opcode.ApplyPhase(currentPhase)
		cell.SetStatePhase(nextPhase)
		changed = true
	}

	return cell, changed
}

/*
HammingDelta computes the total XOR popcount between two values across
all core blocks. Serves as the discrete distance metric for wavefront
convergence detection without allocating a temporary Value.
*/
func (lattice *Lattice) HammingDelta(
	cellA primitive.Value,
	cellB primitive.Value,
) int {
	delta := 0

	for blockIndex := range config.CoreBlocks {
		delta += bits.OnesCount64(cellA.Block(blockIndex) ^ cellB.Block(blockIndex))
	}

	return delta
}

/*
LatticeWithOpcodes injects the shared MacroIndex opcode registry.
*/
func LatticeWithOpcodes(opcodes *macro.MacroIndexServer) latticeOpts {
	return func(lattice *Lattice) {
		lattice.opcodes = opcodes
	}
}
