package program

const (
	InstrDstSpanShift  = 0
	InstrDstStartShift = 6

	InstrASpanShift  = 13
	InstrAStartShift = 19

	InstrBSpanShift  = 26
	InstrBStartShift = 32

	InstrOpcodeShift   = 39
	InstrModeShift     = 43
	InstrTopologyShift = 46

	InstrPredStartShift = 48
	InstrPredCondShift  = 55

	InstrAIndirectShift = 57
	InstrBTypeShift     = 58

	InstrSpanMask  uint64 = 0x3F
	InstrStartMask uint64 = 0x7F
)

const (
	InstrFlagTargetB     uint64 = 1 << 60
	InstrFlagTargetOwner uint64 = 1 << 61
	InstrFlagAFromB      uint64 = 1 << 62
	InstrFlagBFromA      uint64 = 1 << 63
)

const (
	InstrBTypeDirect uint64 = iota
	InstrBTypeIndirect
	InstrBTypeImmediate
	InstrBTypeNext
)

const (
	TopologySelf  = 0
	TopologyNext  = 1
	TopologyFold  = 2
	TopologySpawn = 3
)

const (
	ModeTruth     = 0
	ModePopcnt    = 1
	ModeAnyZero   = 2
	ModeAllOnes   = 3
	ModeGeometric = 4
	ModeEmit      = 5
)

var Opcodes = map[string]uint64{
	"0":  0b0000,
	"&":  0b0001,
	"\\": 0b0010,
	"A":  0b0011,
	"/":  0b0100,
	"B":  0b0101,
	"^":  0b0110,
	"|":  0b0111,
	"~|": 0b1000,
	"==": 0b1001,
	"~B": 0b1010,
	"<-": 0b1011,
	"~A": 0b1100,
	"->": 0b1101,
	"~&": 0b1110,
	"1":  0b1111,

	"compose":  0x10,
	"sandwich": 0x20,
	"reverse":  0x30,
}

var Topologies = map[string]uint64{
	"self":  TopologySelf,
	"next":  TopologyNext,
	"fold":  TopologyFold,
	"spawn": TopologySpawn,
	"emit":  TopologySpawn,
}

func IsGeometricOpcode(opcode uint64) bool {
	switch opcode & 0xF0 {
	case 0x10, 0x20, 0x30:
		return true
	default:
		return false
	}
}

func isFoldOpcode(opcode uint64) bool {
	switch opcode {
	case Opcodes["0"], Opcodes["1"], Opcodes["&"], Opcodes["|"], Opcodes["^"], Opcodes["=="]:
		return true
	default:
		return false
	}
}

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
		((predStart & InstrStartMask) << InstrPredStartShift) |
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
