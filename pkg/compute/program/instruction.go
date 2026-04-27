package program

/*
Instruction bit layout matches the CUDA/Metal ast_decode contract (see
pkg/compute/kernel/cuda/backend.cu). This is the on-wire format HypercubeGossip
executes, not an authoring syntax.
*/

const (
	InstrDstSpanShift   = 0
	InstrDstStartShift  = 6
	InstrASpanShift     = 13
	InstrAStartShift    = 19
	InstrBSpanShift     = 26
	InstrBStartShift    = 32
	InstrOpcodeShift    = 39
	InstrModeShift      = 43
	InstrTopologyShift  = 46
	InstrPredStartShift = 48
	InstrPredCondShift  = 55
	InstrAIndirectShift = 57
	InstrBTypeShift     = 58

	InstrSpanMask  = 0x3f
	InstrStartMask = 0x7f

	InstrFlagTargetB     uint64 = 1 << 60
	InstrFlagTargetOwner uint64 = 1 << 61
	InstrFlagAFromB      uint64 = 1 << 62
	InstrFlagBFromA      uint64 = 1 << 63
)

const (
	ModeTruth     uint64 = 0
	ModePopcnt    uint64 = 1
	ModeAnyZero   uint64 = 2
	ModeAllOnes   uint64 = 3
	ModeGeometric uint64 = 4
	ModeEmit      uint64 = 5
)

const (
	TopologySelf  uint64 = 0
	TopologyNext  uint64 = 1
	TopologyFold  uint64 = 2
	TopologySpawn uint64 = 3
)

const (
	InstrBTypeDirect    uint64 = 0
	InstrBTypeIndirect  uint64 = 1
	InstrBTypeImmediate uint64 = 2
	InstrBTypeNext      uint64 = 3
)

/*
EncodeInstruction packs one resident sweep step. Flags (TargetB, etc.) are OR’d
by callers onto the low 60-bit encoding.
*/
func EncodeInstruction(
	aStart, aSpan, bStart, bSpan, dstStart, dstSpan int,
	opcode, mode, topology, predStart, predCond, aInd, bType uint64,
) uint64 {
	if aSpan <= 0 {
		aSpan = 1
	}
	if bSpan <= 0 {
		bSpan = 1
	}
	if dstSpan <= 0 {
		dstSpan = 1
	}

	encodedOpcode := opcode
	if IsGeometricOpcode(opcode) {
		mode = ModeGeometric
		encodedOpcode = opcode >> 4
	}

	return ((uint64(dstSpan-1) & InstrSpanMask) << InstrDstSpanShift) |
		((uint64(dstStart) & InstrStartMask) << InstrDstStartShift) |
		((uint64(aSpan-1) & InstrSpanMask) << InstrASpanShift) |
		((uint64(aStart) & InstrStartMask) << InstrAStartShift) |
		((uint64(bSpan-1) & InstrSpanMask) << InstrBSpanShift) |
		((uint64(bStart) & InstrStartMask) << InstrBStartShift) |
		((encodedOpcode & 0xF) << InstrOpcodeShift) |
		((mode & 0x7) << InstrModeShift) |
		((topology & 0x3) << InstrTopologyShift) |
		((uint64(predStart) & InstrStartMask) << InstrPredStartShift) |
		((predCond & 0x3) << InstrPredCondShift) |
		((aInd & 0x1) << InstrAIndirectShift) |
		((bType & 0x3) << InstrBTypeShift)
}

func DecodeInstruction(instr uint64) (
	aStart, aSpan, bStart, bSpan, dstStart, dstSpan int,
	opcode, mode, topology, predStart, predCond, aInd, bType uint64,
) {
	dstSpan = int((instr>>InstrDstSpanShift)&InstrSpanMask) + 1
	dstStart = int((instr >> InstrDstStartShift) & InstrStartMask)
	aSpan = int((instr>>InstrASpanShift)&InstrSpanMask) + 1
	aStart = int((instr >> InstrAStartShift) & InstrStartMask)
	bSpan = int((instr>>InstrBSpanShift)&InstrSpanMask) + 1
	bStart = int((instr >> InstrBStartShift) & InstrStartMask)
	opcode = (instr >> InstrOpcodeShift) & 0xF
	mode = (instr >> InstrModeShift) & 0x7
	if mode == ModeGeometric {
		opcode <<= 4
	}
	topology = (instr >> InstrTopologyShift) & 0x3
	predStart = (instr >> InstrPredStartShift) & InstrStartMask
	predCond = (instr >> InstrPredCondShift) & 0x3
	aInd = (instr >> InstrAIndirectShift) & 0x1
	bType = (instr >> InstrBTypeShift) & 0x3

	return
}

func IsGeometricOpcode(opcode uint64) bool {
	return opcode&0xF0 == 0x10 || opcode&0xF0 == 0x20 || opcode&0xF0 == 0x30
}
