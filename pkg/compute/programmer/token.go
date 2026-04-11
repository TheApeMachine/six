package programmer

/*
RegionType names coarse Value regions for later lowering from region refs.

Source text uses string refs (e.g. tokens[0,16]); lowering maps those into
the packed Value layout via RegionRef.
*/
type RegionType uint8

const (
	TokenRegion RegionType = iota
	ProgramRegion
	SignalsRegion
	ContextRegion
	GradientRegion
	MetaRegion
	ReservedRegion
	PrevRegion
	NextRegion
	IDRegion
	AffinityRegion
)

/*
OperationType is the 4-bit universal bitwise opcode space (value.opcodes).

Source mnemonics may name non-truth-table ops (e.g. popcount); those stay on
Token as strings until lowering selects a concrete kernel path.
*/
type OperationType uint8

const (
	FALSE      OperationType = 0b0000
	AND        OperationType = 0b0001
	AANDNOTB   OperationType = 0b0010
	A          OperationType = 0b0011
	NOTANDB    OperationType = 0b0100
	B          OperationType = 0b0101
	XOR        OperationType = 0b0110
	OR         OperationType = 0b0111
	NOR        OperationType = 0b1000
	XNOR       OperationType = 0b1001
	NOTB       OperationType = 0b1010
	IFBTHENA   OperationType = 0b1011
	NOTA       OperationType = 0b1100
	IFA_THEN_B OperationType = 0b1101
	NAND       OperationType = 0b1110
	TRUE       OperationType = 0b1111
)

/*
ExecutionMode controls how the ALU signal output is folded back into the
dst region after a frame runs. Accumulate XORs signals into dst so a chain
of lines builds up state; Reduce collapses signals to a popcount written
into dst[start] and leaves the rest of the span untouched.
*/
type ExecutionMode uint8

const (
	ModeAccumulate ExecutionMode = iota
	ModeReduce
)

/*
Token is one source line after parse: parsed region refs for operands and
destination, a mnemonic op, and an execution mode. SrcA/SrcB/Dst keep the
original source strings for diagnostics; SrcARef/SrcBRef/DstRef are the
resolved slices Staging/Writeback consume.

Syntax matches cmd/cfg/config.yml programs: blocks, e.g.:

	srcA srcB dst op mode
	tokens[0,16] tokens[0,16] affinity[0,5] xor accumulate
*/
type Token struct {
	SrcA    string
	SrcB    string
	Dst     string
	Op      string
	Mode    string
	SrcARef RegionRef
	SrcBRef RegionRef
	DstRef  RegionRef
	ModeBit ExecutionMode
}
