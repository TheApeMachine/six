package kernel

/*
Value frame word layout for ALU dispatch. Words are uint64 indices into the
128-word Value.
*/
const (
	TokensStartWord           = 0
	ProgramStartWord          = 16
	SignalsStartWord          = 24
	ContextStartWord          = 32
	GradientStartWord         = 40
	PropertiesStartWord       = 48
	PropertiesTTLWord         = 51 // properties[3]
	PropertiesNoiseWord       = 52 // properties[4]
	PropertiesProbeStateWord  = 53 // properties[5], explicit runtime probe ABI only
	PropertiesProbeWindowWord = 54 // properties[6], PackRegionRef over token words
	PropertiesProbeDepthWord  = 55 // properties[7], final re-stabilization depth
	PropertiesCommunityWord   = 56 // properties[8], stamped by mesh.Field community routing
	PropertiesStatusWord      = 57 // properties[9], firmware lifecycle: 0=raw, READY=bootstrapped

	// StatusRaw is the zero-value default: the Value has not been through
	// the firmware chain yet. The field may store it tentatively but should
	// expect a follow-up write once bootstrap completes.
	StatusRaw   uint64 = 0
	// StatusReady means the firmware chain (link → affinity → resident)
	// has run to completion and the Value's regions are fully populated.
	StatusReady uint64 = 1

	AssetStartWord = 72
	SchedulingNextProgramWord = 117
	// ReservedStartWord is the legacy name for the asset scratch region base word.
	ReservedStartWord = AssetStartWord
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
	// OpcodeCopyMaskMerge copies srcA into dst with a per-word mask taken from
	// srcB: dst = (srcA & srcB) | (dst & ^srcB). Bypasses the 16-rotation sweep.
	OpcodeCopyMaskMerge uint64 = 0x50

	// OpcodeEmitClone reserves a new arena slot, copies the frame, perturbs the
	// affinity lane from properties noise, and records (parent, child) slots for
	// the host drain path. Implemented on GPU substrates.
	OpcodeEmitClone uint64 = 0x60

	// TTLExpiredSentinel is stored in PropertiesTTLWord when an ephemeral Value's
	// TTL has just reached zero in-kernel; finalize skips orchestrator publish.
	TTLExpiredSentinel uint64 = 1 << 63

	// PropertiesRefutationTargetWord holds another Value's ID (word 122 style)
	// for native falsification checks against in-band XOR signals.
	PropertiesRefutationTargetWord = 49

	// FalsifiedBitNoiseWord is ORed into PropertiesNoiseWord when refutation fires.
	FalsifiedBitNoiseWord uint64 = 1 << 62

	// RefutationOneRunThreshold is the minimum longest one-run length (bits)
	// across the eight signal words to count as a Popper-style refutation streak.
	RefutationOneRunThreshold = 48

	// OpcodeXOR is the universal-bitwise low nibble for XOR (truth table 0x6).
	OpcodeXOR uint64 = 0x06

	NearestAffinityBatchWord = 124

	NearestAffinityCandidatesStartWord = AssetStartWord
	MaxNearestAffinityCandidates       = 256

	SignalBestIdxOffset  = 0
	SignalBestDistOffset = 1
)

const (
	CausalProbeKindNone uint8 = iota
	CausalProbeKindHub
)

const (
	CausalProbeStatusInactive uint8 = iota
	CausalProbeStatusActive
	CausalProbeStatusSettled
	CausalProbeStatusCeiling
)

/*
PackProbeState encodes probe kind and lifecycle status into one properties word.
*/
func PackProbeState(kind uint8, status uint8) uint64 {
	return uint64(kind) | (uint64(status) << 8)
}

/*
ProbeKind returns the encoded probe kind from a probe-state word.
*/
func ProbeKind(word uint64) uint8 {
	return uint8(word & 0xff)
}

/*
ProbeStatus returns the encoded probe lifecycle status from a probe-state word.
*/
func ProbeStatus(word uint64) uint8 {
	return uint8((word >> 8) & 0xff)
}

/*
Program region wire layout. The DSL compiler packs one line's operand map
into these six words so the substrate can read srcA / srcB / dst directly
out of the Value without a Go-side stage/writeback wrapper. Every field is
an absolute word index (0..127), not a region-local offset — the compiler
resolves the RegionRef before packing so the ALU never has to know about
region names at run time.

Word 0 still holds the legacy opcode byte in its low 8 bits so the
geometric / nearest-affinity / region-program dispatch at the top of
Backend.Execute keeps working unchanged.
*/
const (
	ProgramOpcodeWord = ProgramStartWord     // legacy opcode byte + geometric gate
	ProgramRotTabWord = ProgramStartWord + 1 // 16-nibble rotation opcode table
	ProgramModeWord   = ProgramStartWord + 2 // low byte: ExecutionMode, next byte: lowered contract kind
	ProgramSrcAWord   = ProgramStartWord + 3 // srcA: aStart low32 | aSpan high32
	ProgramSrcBWord   = ProgramStartWord + 4 // srcB: bStart low32 | bSpan high32
	ProgramDstWord    = ProgramStartWord + 5 // dst:  dStart low32 | dSpan high32
)

const (
	ProgramContractShift = 8
	ProgramContractMask  = uint64(0xFF) << ProgramContractShift

	ProgramContractUnknown     = uint64(0)
	ProgramContractExactBinary = uint64(1)
	ProgramContractSweepSignal = uint64(2)
	ProgramContractReduce      = uint64(3)
	ProgramContractGeometric   = uint64(4)
)

/*
PackRegionRef packs an absolute word start and a span into one uint64 so a
program region word can describe one operand lane. The low 32 bits hold
the start and the high 32 bits hold the span. Values larger than 32 bits
are masked off — the Value is only 128 words wide and regions never need
more span than fits in a uint32.
*/
func PackRegionRef(start int, span int) uint64 {
	return uint64(uint32(start)) | (uint64(uint32(span)) << 32)
}

/*
UnpackRegionRef is the inverse of PackRegionRef.
*/
func UnpackRegionRef(word uint64) (start int, span int) {
	return int(uint32(word)), int(uint32(word >> 32))
}

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

/*
IsCopyMaskMergeOpcode reports whether the low opcode byte selects the masked
word-copy lane (no rotation sweep).
*/
func IsCopyMaskMergeOpcode(opcode uint64) bool {
	return opcode&0xFF == OpcodeCopyMaskMerge
}

/*
IsEmitCloneOpcode reports whether the frame requests in-arena cloning on GPU.
*/
func IsEmitCloneOpcode(opcode uint64) bool {
	return opcode&0xFF == OpcodeEmitClone
}
