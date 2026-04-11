package kernel

/*
Value frame word layout for ALU dispatch. Words are uint64 indices into the
128-word Value.
*/
const (
	TokensStartWord   = 0
	ProgramStartWord  = 16
	SignalsStartWord  = 24
	ContextStartWord  = 32
	GradientStartWord = 40
	MetaStartWord     = 48
	ReservedStartWord = 56
	PrevStartWord     = 120
	NextStartWord     = 121
	IDStartWord       = 122
	AffinityStartWord = 123

	OpcodeBooleanMask   uint64 = 0x0F
	OpcodeGeometricMask uint64 = 0xF0

	OpcodeGeometricCompose  uint64 = 0x10
	OpcodeGeometricSandwich uint64 = 0x20
	OpcodeGeometricReverse  uint64 = 0x30
	OpcodeRegionProgram     uint64 = 0x40

	// OpcodeXOR is the universal-bitwise low nibble for XOR (truth table 0x6).
	OpcodeXOR uint64 = 0x06

	NearestAffinityBatchWord = 124

	NearestAffinityCandidatesStartWord = 56
	MaxNearestAffinityCandidates       = 256

	SignalBestIdxOffset  = 0
	SignalBestDistOffset = 1
)

/*
IsGeometricOpcode reports whether the full opcode byte should route to the PGA lane.
*/
func IsGeometricOpcode(opcode uint64) bool {
	switch opcode & OpcodeGeometricMask {
	case OpcodeGeometricCompose, OpcodeGeometricSandwich, OpcodeGeometricReverse:
		return true
	}

	return false
}
