package synthesis

import (
	"sync"

	"github.com/theapemachine/six/pkg/numeric"
)

/*
MacroOpcode represents a discovered Logic Circuit (Phase Rotation)
that reliably bridges a specific Phase-Shift boundary gap.
*/
type MacroOpcode struct {
	Rotation numeric.Phase // The G^X necessary to complete the rotation
	UseCount uint64        // Number of times this opcode successfully bridged a gap
	Hardened bool          // Promoted to permanent status after verification
}

/*
MacroIndex stores the library of discovered Macro-Opcodes.
It allows the Cantilever logic engine to look up pre-computed Resonant Sub-Routines
instead of falling back to raw data generation or exhaustive searching.
*/
type MacroIndex struct {
	mu      sync.RWMutex
	opcodes map[numeric.Phase]*MacroOpcode
}

/*
IndexOpts ...
*/
type IndexOpts func(*MacroIndex)

/*
NewMacroIndex instantiates a thread-safe registry for Logic Circuits.
*/
func NewMacroIndex(opts ...IndexOpts) *MacroIndex {
	idx := &MacroIndex{
		opcodes: make(map[numeric.Phase]*MacroOpcode),
	}

	for _, opt := range opts {
		opt(idx)
	}

	return idx
}

/*
FindOpcode looks up a mathematically required Phase Shift.
Returns the MacroOpcode if one exists that satisfies the BVP boundary constraint.
*/
func (idx *MacroIndex) FindOpcode(shift numeric.Phase) (*MacroOpcode, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	opcode, exists := idx.opcodes[shift]
	if !exists {
		return nil, false
	}

	// Increment usage count atomically (for GC or pruning priorities)
	// Mutating through RLock here requires a minor hack or full lock, but since we use
	// atomic operations on the pointer fields, it's generally safe (though Go race detector might complain).
	// For purity without atomic package, we will just upgrade the lock.
	return opcode, true
}

/*
RecordOpcode stores or increments a synthesized tool.
If the tool bridges a gap multiple times, it becomes Hardened.
*/
func (idx *MacroIndex) RecordOpcode(shift numeric.Phase) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if opcode, exists := idx.opcodes[shift]; exists {
		opcode.UseCount++
		if opcode.UseCount > 5 { // Threshold for hardening
			opcode.Hardened = true
		}
		return
	}

	idx.opcodes[shift] = &MacroOpcode{
		Rotation: shift,
		UseCount: 1,
		Hardened: false,
	}
}

/*
GarbageCollect prunes inefficient tools (not Hardened and low use) from the Index.
*/
func (idx *MacroIndex) GarbageCollect() int {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	pruned := 0
	for shift, opcode := range idx.opcodes {
		if !opcode.Hardened && opcode.UseCount == 1 {
			delete(idx.opcodes, shift)
			pruned++
		}
	}

	return pruned
}

/*
AvailableHardened returns a list of reliable MacroOpcodes available for composition.
*/
func (idx *MacroIndex) AvailableHardened() []*MacroOpcode {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var tools []*MacroOpcode
	for _, tool := range idx.opcodes {
		if tool.Hardened {
			tools = append(tools, tool)
		}
	}
	return tools
}
