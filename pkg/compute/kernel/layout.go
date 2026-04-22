/*
Package kernel hosts shared word indices for Value frames. Layout matches
pkg/core/config.go default ValueRegionConfig (program words 16–23, etc.).

Program words are now packed 64-bit instructions: each word is a complete
universal-bitwise sweep specifier (srcA + srcB + dst spans, opcode nibble,
mode). The encoding lives in pkg/compute/program; the decoder lives in
pkg/compute/kernel/cpu/wordblock_universal.go. There is no separate "header
word" — a zero word terminates the program.
*/
package kernel

// Absolute word indices for the canonical Value frame layout.
//
// Tokens 0–15, Program 16–31 (16 packed instruction words), Signals 32–39,
// Context 40–47, Gradient 48–55, Properties 56–71, Asset 72–119, then Prev,
// Next, ID, Affinity 123–127. The scheduler word (117) and the kernel frame
// metadata (118–119) live inside the Asset region as before.
const (
	TokensStartWord     = 0
	ProgramStartWord    = 16
	SignalsStartWord    = 32
	ContextStartWord    = 40
	GradientStartWord   = 48
	PropertiesStartWord = 56
	AssetStartWord      = 72
	PrevStartWord       = 120
	NextStartWord       = 121
	ValueIDStartWord    = 122
	AffinityStartWord   = 123
)

const OpcodeBooleanMask = 0xF

// OpcodeAND is the 4-bit truth-table nibble for AND (same convention as the
// DSL lowering in pkg/compute/program).
const OpcodeAND = 0x1

// OpcodeXOR is the 4-bit truth-table nibble for XOR (same convention as the
// DSL lowering in pkg/compute/program).
const OpcodeXOR = 0x6

/*
PackRegionRef packs (start, span) into a single uint64 — low 32 bits hold
start, high 32 bits hold span. Retained for tooling (visualizer telemetry,
probe-window layout helpers) that still uses this pre-bytecode descriptor
shape; the universal-bitwise kernel itself no longer reads it.
*/
func PackRegionRef(start, span int) uint64 {
	return uint64(start) | uint64(span)<<32
}

// UnpackRegionRef reverses PackRegionRef.
func UnpackRegionRef(regionRef uint64) (start, span int) {
	return int(regionRef & 0xffffffff), int(regionRef >> 32)
}

