// Package kernel hosts shared word indices for Value frames. Layout matches
// pkg/core/config.go default ValueRegionConfig (program words 16–23, etc.).
package kernel

// Program region indices (absolute word offsets in a Value).
const (
	ProgramStartWord  = 16
	ProgramOpcodeWord = ProgramStartWord + 0
	ProgramModeWord   = ProgramStartWord + 1
	ProgramRotTabWord = ProgramStartWord + 2
	ProgramSrcAWord   = ProgramStartWord + 3
	ProgramSrcBWord   = ProgramStartWord + 4
	ProgramDstWord    = ProgramStartWord + 5

	TokensStartWord   = 0
	SignalsStartWord  = 24
	AssetStartWord    = 64
	PrevStartWord     = 120
	AffinityStartWord = 123
)

const OpcodeBooleanMask = 0xF

// OpcodeXOR is the 4-bit truth-table nibble for XOR (same convention as DSL lowering).
const OpcodeXOR = 0x6

// PackRegionRef packs (start, span) for ALU operand descriptors.
func PackRegionRef(start, span int) uint64 {
	return uint64(start) | uint64(span)<<32
}

// UnpackRegionRef unpacks PackRegionRef.
func UnpackRegionRef(w uint64) (start, span int) {
	return int(w & 0xffffffff), int(w >> 32)
}
