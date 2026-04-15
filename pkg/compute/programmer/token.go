package programmer

import "github.com/theapemachine/six/pkg/primitive"

/*
OperationType is the 4-bit universal bitwise opcode space (value.opcodes).
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

	COMPOSE  OperationType = 0x10
	SANDWICH OperationType = 0x20
	REVERSE  OperationType = 0x30
)

/*
ExecutionMode controls how the ALU folds signal output into the dst region.
*/
type ExecutionMode uint8

const (
	ModeAccumulate ExecutionMode = iota
	ModeReduce
)

/*
RegionRef is one absolute word span inside a Value. Source uses region-local
refs like tokens[0,16]; the parser resolves them before lowering.
*/
type RegionRef struct {
	Region primitive.RegionType
	Start  int
	Span   int
}

/*
NewRegionRef returns a ref covering the whole configured region.
*/
func NewRegionRef(region primitive.RegionType) RegionRef {
	start, span := region.WordExtent()

	return RegionRef{
		Region: region,
		Start:  start,
		Span:   span,
	}
}

/*
FullRegionRef is kept as the shorter test/helper spelling.
*/
func FullRegionRef(region primitive.RegionType) RegionRef {
	return NewRegionRef(region)
}

/*
WordExtent returns the absolute span consumed by kernel.PackRegionRef.
*/
func (ref RegionRef) WordExtent() (start int, span int) {
	return ref.Start, ref.Span
}

/*
Token is one parsed source line: region refs, op mnemonic, execution mode.
*/
type Token struct {
	SrcA RegionRef
	SrcB RegionRef
	Dst  RegionRef
	Op   OperationType
	Mode ExecutionMode
}
