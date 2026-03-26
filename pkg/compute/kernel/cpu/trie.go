package cpu

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"github.com/spf13/viper"
	"github.com/theapemachine/six/pkg/core"
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
	v[core.Cfg.StateIndex] = 1

	// Initialize affinity for clustering
	v.InitializeAffinity()

	// Delegate all opcode assignments to InstallProgram (single source of truth).
	InstallProgram(v, programType)

	return v
}

// InstallProgram installs a named program into a Value.
// This is the single source of truth for all ProgramType opcode assignments.
func InstallProgram(value *primitive.Value, programType ProgramType) {
	switch programType {
	case ProgramShatter:
		InstallProgramFrom(value, GetProgram("shatter"))
	case ProgramXOR:
		InstallProgramFrom(value, GetProgram("xor"))
	case ProgramMerge:
		InstallProgramFrom(value, GetProgram("merge"))
	case ProgramProjectA:
		InstallProgramFrom(value, GetProgram("projecta"))
	case ProgramProjectB:
		InstallProgramFrom(value, GetProgram("projectb"))
	default:
		// Fallback: simple OR/accumulate passthrough
		InstallProgramFrom(value, GetProgram("merge"))
	}
}

/*
EncodeTrieBatch packs byte sequences into Region0 TokenIDs and IDs.
This is still useful for testing and initialization.
*/
func (backend *Backend) EncodeTrieBatch(
	sequences [][]byte, dst unsafe.Pointer, numValues uint32,
) {
	if uint32(len(sequences)) < numValues {
		numValues = uint32(len(sequences))
	}
	ds := unsafe.Slice((*[primitive.Words]uint64)(dst), numValues)

	for v := uint32(0); v < numValues; v++ {
		value := (*primitive.Value)(unsafe.Pointer(&ds[v]))
		seq := sequences[v]

		for i, b := range seq {
			if i >= int(core.Cfg.TokenIndex) {
				break
			}
			value.SetTokenID(i, primitive.Tokenize(b, uint64(i)))
		}

		value.SetValueID(uint64(v + 1))
	}
}

// Program represents a sequence of operations that can be installed into a Value.
type Program []uint32

// GetProgram reads a named program from config.yml and compiles it using
// the native 3-token Polish Notation compiler. Programs in config are
// multi-line strings of the form: src dest gate
func GetProgram(name string) Program {
	// Ensure Viper can find the configuration if we are running in headless tests
	if viper.ConfigFileUsed() == "" {
		_, b, _, _ := runtime.Caller(0)
		projectRoot := filepath.Join(filepath.Dir(b), "../../../../")
		viper.SetConfigFile(filepath.Join(projectRoot, "cmd/cfg/config.yml"))
		_ = viper.ReadInConfig()
	}

	key := "programs." + strings.ToLower(name)
	source := viper.GetString(key)
	if source == "" {
		return Program{0}
	}

	kernel, err := primitive.Compile(source)
	if err != nil {
		fmt.Printf("cpu: program %q compile error: %v\n", name, err)
		return Program{0}
	}

	return Program(kernel)
}

// InstallProgramFrom installs a Program (sequence of opcodes) into a Value.
func InstallProgramFrom(value *primitive.Value, program Program) {
	for i, op := range program {
		if i >= core.Cfg.MaxPC {
			break
		}
		value.WriteVMInstruction(i, op)
	}
}

// ComposePrograms creates a new program by concatenating multiple programs.
// This allows building complex operations from simpler ones.
// Intermediate HALT (0) instructions from each sub-program are stripped before
// appending the next program so they cannot stop execution prematurely.
// A single HALT is always appended at the very end.
func ComposePrograms(programs ...Program) Program {
	var result Program
	for _, p := range programs {
		// Strip trailing HALTs from the accumulating result before appending
		// the next program so intermediate HALTs don't stop execution early.
		for len(result) > 0 && result[len(result)-1] == 0 {
			result = result[:len(result)-1]
		}
		result = append(result, p...)
	}
	// Ensure the composed program ends with exactly one HALT.
	for len(result) > 0 && result[len(result)-1] == 0 {
		result = result[:len(result)-1]
	}
	result = append(result, 0)
	return result
}

// AnalyzeProgram returns a human-readable description of what a program does.
func AnalyzeProgram(program Program) string {
	var desc string
	for i, op := range program {
		if op == 0 {
			break
		}
		opcode := op & 0xF
		desc += fmt.Sprintf("Tick %d: VM Opcode 0b%04b\n", i, opcode)
	}
	return desc
}

// CreateComplexValue creates a Value with a complex composed program.
// This demonstrates the full power of the in-band programming system.
func CreateComplexValue(data string, programs ...Program) *primitive.Value {
	v := primitive.NewValue()
	v.Write([]byte(data))
	v.SetValueID(1000)
	v[core.Cfg.StateIndex] = 1
	v.InitializeAffinity()

	// If no programs provided, use shatter as default
	if len(programs) == 0 {
		InstallProgramFrom(v, GetProgram("shatter"))
	} else if len(programs) == 1 {
		InstallProgramFrom(v, programs[0])
	} else {
		// Compose multiple programs
		composed := ComposePrograms(programs...)
		InstallProgramFrom(v, composed)
	}

	return v
}
