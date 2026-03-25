package cpu

import (
	"fmt"
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
This file contains minimal support functions for the new in-band architecture:

- AffinityMatch(): bitwise affinity-based pairing using Region 2
- decodeBatchFrames(): utility for converting wire frames to Values
- EncodeTrieBatch(): test/initialization helper

Region 4 (Link) is available for temporary grouping/linking of Values.
All the old "residents", CancellationSpan, buildEmittedValues, and related
orchestration code has been removed.
*/

// AffinityMatch returns true if two Values should be paired for ALU processing
// based on their affinity masks (Region 2). This replaces the old token-based
// longestCancellationSpan logic.
func AffinityMatch(a, b *primitive.Value, threshold int) bool {
	affA := a.AffinityMask()
	affB := b.AffinityMask()
	overlap := affA & affB
	// Use the existing Popcount but on a temporary value containing the overlap
	tmp := primitive.Value{}
	tmp[0] = overlap
	return Popcount(&tmp, 0, 64) > threshold
}

func decodeBatchFrames(batch []byte) []primitive.Value {
	count := len(batch) / primitive.ByteSize
	values := make([]primitive.Value, 0, count)

	for offset := 0; offset+primitive.ByteSize <= len(batch); offset += primitive.ByteSize {
		frame := primitive.BytesToValue(batch[offset : offset+primitive.ByteSize])
		values = append(values, *frame)
	}

	return values
}

// LinkValues demonstrates the Region 4 temporary linking capability.
// It sets up a chain of Values using the new linking primitives.
func LinkValues(values []*primitive.Value) {
	for i := 0; i < len(values)-1; i++ {
		values[i].SetLink(values[i+1].ValueID())
	}
	if len(values) > 0 {
		// Last value can point back or to a sentinel
		values[len(values)-1].SetLink(0)
	}
}

// ShowLinkInfo returns information about the linking structure for debugging.
func ShowLinkInfo(values []*primitive.Value) string {
	var result string
	for i, v := range values {
		link := v.Link()
		tok := primitive.DecodeTokensToText(v)
		result += fmt.Sprintf("Value %d: %q -> Link: %d\n", i, tok, link)
	}
	return result
}

// ProgramType represents different types of structuring programs.
type ProgramType string

const (
	ProgramShatter  ProgramType = "shatter"   // AND + A&^B + B&^A sequence
	ProgramXOR      ProgramType = "xor"       // Simple XOR for annihilation
	ProgramMerge    ProgramType = "merge"     // OR/accumulate
	ProgramProjectA ProgramType = "project_a" // Keep only A component
	ProgramProjectB ProgramType = "project_b" // Keep only B component
)

// CreateValueWithProgram is a convenience function for creating a Value
// with both data and a structuring program.
func CreateValueWithProgram(data string, programType ProgramType) *primitive.Value {
	v := primitive.NewValue()
	v.Write([]byte(data))
	v.SetValueID(1000) // arbitrary ID for demo
	v[primitive.StateSlotIndex] = 1

	// Initialize affinity for clustering
	v.InitializeAffinity()

	// Set up program based on type
	switch programType {
	case ProgramShatter:
		v.InstallShatterProgram()
	case ProgramXOR:
		// Simple XOR program for annihilation (Roy test)
		v.SetProgramOp(0, 0b0110) // XOR
		v.SetProgramOp(1, 0b0000) // HALT
	case ProgramMerge:
		// OR/accumulate - merges information
		v.SetProgramOp(0, 0b1110) // OR
		v.SetProgramOp(1, 0b0000) // HALT
	case ProgramProjectA:
		// Keep only component from A (A & ~B)
		v.SetProgramOp(0, 0b0010) // A AND NOT B
		v.SetProgramOp(1, 0b0000) // HALT
	case ProgramProjectB:
		// Keep only component from B (B & ~A)
		v.SetProgramOp(0, 0b0100) // B AND NOT A
		v.SetProgramOp(1, 0b0000) // HALT
	default:
		// Default: simple passthrough with OR
		v.SetProgramOp(0, 0b1110) // OR/accumulate
		v.SetProgramOp(1, 0b0000) // HALT
	}

	return v
}

// InstallProgram installs a named program into a Value.
// This provides a more flexible way to set up structuring programs.
func InstallProgram(value *primitive.Value, programType ProgramType) {
	switch programType {
	case ProgramShatter:
		value.InstallShatterProgram()
	case ProgramXOR:
		value.SetProgramOp(0, 0b0110) // XOR
		value.SetProgramOp(1, 0b0000) // HALT
	case ProgramMerge:
		value.SetProgramOp(0, 0b1110) // OR
		value.SetProgramOp(1, 0b0000) // HALT
	case ProgramProjectA:
		value.SetProgramOp(0, 0b0010) // A AND NOT B
		value.SetProgramOp(1, 0b0000) // HALT
	case ProgramProjectB:
		value.SetProgramOp(0, 0b0100) // B AND NOT A
		value.SetProgramOp(1, 0b0000) // HALT
	}
}

/*
EncodeTrieBatch packs byte sequences into Region0 TokenIDs and IDs.
This is still useful for testing and initialization.
*/
func (backend *Backend) EncodeTrieBatch(
	sequences [][]byte, dst unsafe.Pointer, numValues uint32,
) {
	ds := unsafe.Slice((*[primitive.Words]uint64)(dst), numValues)

	for v := uint32(0); v < numValues; v++ {
		value := (*primitive.Value)(unsafe.Pointer(&ds[v]))
		seq := sequences[v]

		for i, b := range seq {
			if i >= primitive.Region0TokenCount {
				break
			}
			value.SetTokenID(i, primitive.Tokenize(b, uint64(i)))
		}

		value.SetValueID(uint64(v + 1))
	}
}

// Program represents a sequence of operations that can be installed into a Value.
type Program []uint8

// Common program templates for different operations.
var (
	// ShatterProgram implements the classic "shared label + remainders" pattern
	ShatterProgram = Program{0b1000, 0b0010, 0b0100, 0b0000} // AND, A&^B, B&^A, HALT

	// XorProgram for prompt-style annihilation
	XorProgram = Program{0b0110, 0b0000} // XOR, HALT

	// MergeProgram for accumulating information
	MergeProgram = Program{0b1110, 0b0000} // OR, HALT

	// ProjectAProgram keeps only A's unique components
	ProjectAProgram = Program{0b0010, 0b0000} // A AND NOT B, HALT

	// ProjectBProgram keeps only B's unique components
	ProjectBProgram = Program{0b0100, 0b0000} // B AND NOT A, HALT
)

// InstallProgramFrom installs a Program (sequence of opcodes) into a Value.
func InstallProgramFrom(value *primitive.Value, program Program) {
	for i, op := range program {
		if i >= 64 {
			break // can't exceed 64 operations
		}
		value.SetProgramOp(i, op)
	}
}

// ComposePrograms creates a new program by concatenating multiple programs.
// This allows building complex operations from simpler ones.
func ComposePrograms(programs ...Program) Program {
	var result Program
	for _, p := range programs {
		result = append(result, p...)
	}
	// Ensure it ends with HALT if not already present
	if len(result) == 0 || result[len(result)-1] != 0 {
		result = append(result, 0)
	}
	return result
}

// AnalyzeProgram returns a human-readable description of what a program does.
func AnalyzeProgram(program Program) string {
	var desc string
	for i, op := range program {
		if op == 0 {
			break
		}
		switch op {
		case 0b1000:
			desc += fmt.Sprintf("Tick %d: AND (shared label)\n", i)
		case 0b0010:
			desc += fmt.Sprintf("Tick %d: A AND NOT B (A remainder)\n", i)
		case 0b0100:
			desc += fmt.Sprintf("Tick %d: B AND NOT A (B remainder)\n", i)
		case 0b0110:
			desc += fmt.Sprintf("Tick %d: XOR (annihilation)\n", i)
		case 0b1110:
			desc += fmt.Sprintf("Tick %d: OR (merge/accumulate)\n", i)
		default:
			desc += fmt.Sprintf("Tick %d: Op 0b%04b\n", i, op)
		}
	}
	return desc
}

// CreateComplexValue creates a Value with a complex composed program.
// This demonstrates the full power of the in-band programming system.
func CreateComplexValue(data string, programs ...Program) *primitive.Value {
	v := primitive.NewValue()
	v.Write([]byte(data))
	v.SetValueID(1000)
	v[primitive.StateSlotIndex] = 1
	v.InitializeAffinity()

	// If no programs provided, use shatter as default
	if len(programs) == 0 {
		InstallProgramFrom(v, ShatterProgram)
	} else if len(programs) == 1 {
		InstallProgramFrom(v, programs[0])
	} else {
		// Compose multiple programs
		composed := ComposePrograms(programs...)
		InstallProgramFrom(v, composed)
	}

	return v
}
