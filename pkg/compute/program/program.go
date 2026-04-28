/*
Package program is the host-facing entry point for lowering DSL blocks
into ALU instruction slabs. It is a thin re-export of the new compiler
package — kept under this name so the config / primitive / GPU-backend
import paths stay stable while the rest of the rewrite lands. Anything
that used to live here as legacy plumbing (predicate device specs,
session-scoped predicate numbering, the spawn topology code) has been
removed because the new ALU resolves predicates inline.
*/
package program

import (
	"context"

	"github.com/theapemachine/six/pkg/compute/compiler"
)

/*
Layout snapshots the spatial layout the host hands to the compiler.
The current compiler derives all region offsets from its own canonical
constants, so this struct is now informational; callers can keep
populating it for diagnostics without affecting compilation.
*/
type Layout struct {
	Regions     map[string]RegionExtent
	Properties  map[string]int
	Opcodes     map[string]uint64
	StatusValue map[string]uint64
}

// RegionExtent describes one named region in absolute word coordinates.
type RegionExtent struct {
	Start int
	Words int
	Bits  uint64
}

// ConstantInit re-exports compiler.ConstantInit so config can hold the
// lowering artifact without taking a direct compiler dep.
type ConstantInit = compiler.ConstantInit

// Substitution re-exports compiler.Substitution so InstallFirmware can
// patch operands at install time without taking a direct compiler dep.
type Substitution = compiler.Substitution

// Compiled re-exports compiler.Compiled with a slice-shaped Words view
// for callers that want the bytes without a fixed-size array.
type Compiled struct {
	Words         []uint64
	Constants     []ConstantInit
	Substitutions []Substitution
	MaskTrueWord  uint64
}

/*
Compile lowers a single named DSL block. Errors are propagated; legacy
syntax that does not parse fails loudly so the program library is
forced to align with the new dialect rather than silently degrading.
*/
func Compile(_ context.Context, source string, _ Layout) (Compiled, error) {
	result, err := compiler.Compile(source)
	if err != nil {
		return Compiled{}, err
	}

	words := make([]uint64, 0, len(result.Words))
	for _, word := range result.Words {
		words = append(words, word)
	}

	return Compiled{
		Words:         words,
		Constants:     result.Constants,
		Substitutions: result.Substitutions,
		MaskTrueWord:  result.MaskTrueWord,
	}, nil
}

/*
Bit-layout constants. They mirror the packing performed by
compiler.Builder.Pack so any code that decodes compiled words sees the
same wire format the kernel decodes.
*/
const (
	InstrOpcodeShift    = 0
	InstrAStartShift    = 4
	InstrASpanShift     = 11
	InstrBStartShift    = 18
	InstrBSpanShift     = 25
	InstrDstStartShift  = 32
	InstrDstSpanShift   = 39
	InstrPredStartShift = 46 // mask-word offset
	InstrModeShift      = 53 // 1 = write target is the popped B frame
	InstrEmitShift      = 54
	InstrTopologyShift  = 55
	InstrPredBitShift   = 57 // 1 = popcount-driven predicate instruction
	InstrPredCondShift  = 58 // predicate=1: comparison/reduction; predicate=0: SrcB rot8 steps
	InstrSrcAFromBShift = 61 // 1 = read SrcA from popped frame too

	InstrSpanMask  = 0x7F
	InstrStartMask = 0x7F

	// Vestigial fields retained for visualizer bindings; both are zero
	// under the new ALU and should be removed once the JS side is
	// regenerated.
	InstrAIndirectShift = 0
	InstrBTypeShift     = 0
)

/*
Opcodes is the canonical name → 4-bit truth-table mapping.
*/
var Opcodes = map[string]uint64{
	"false":    0x0,
	"and":      0x1,
	"aandnotb": 0x2,
	"a":        0x3,
	"notandb":  0x4,
	"b":        0x5,
	"xor":      0x6,
	"or":       0x7,
	"nor":      0x8,
	"xnor":     0x9,
	"notb":     0xA,
	"ifbthena": 0xB,
	"nota":     0xC,
	"ifathenb": 0xD,
	"nand":     0xE,
	"true":     0xF,
}

/*
DecodeInstruction unpacks a 64-bit instruction back into its field-wise
components. Returns the canonical 13-tuple shape the visualizer codegen
already consumes; the trailing aIndirect / bType slots are always zero.
*/
func DecodeInstruction(word uint64) (
	aStart, aSpan, bStart, bSpan, dstStart, dstSpan, opcode, mode, topology, predStart, predCond, aIndirect, bType, predicate, emit, srcAFromB, stage, popEnd uint64,
) {
	opcode = word & 0xF
	aStart = (word >> InstrAStartShift) & InstrStartMask
	aSpan = ((word >> InstrASpanShift) & InstrSpanMask) + 1
	bStart = (word >> InstrBStartShift) & InstrStartMask
	bSpan = ((word >> InstrBSpanShift) & InstrSpanMask) + 1
	dstStart = (word >> InstrDstStartShift) & InstrStartMask
	dstSpan = ((word >> InstrDstSpanShift) & InstrSpanMask) + 1
	predStart = (word >> InstrPredStartShift) & InstrStartMask
	mode = (word >> InstrModeShift) & 1
	emit = (word >> InstrEmitShift) & 1
	topology = (word >> InstrTopologyShift) & 3
	predicate = (word >> InstrPredBitShift) & 1
	predCond = (word >> InstrPredCondShift) & 7
	srcAFromB = (word >> InstrSrcAFromBShift) & 1
	stage = (word >> 62) & 1
	popEnd = (word >> 63) & 1
	aIndirect = 0
	bType = 0

	return
}
