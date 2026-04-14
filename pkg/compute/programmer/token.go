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
Token is one parsed source line: region refs, op mnemonic, execution mode.
*/
type Token struct {
	SrcA primitive.RegionType
	SrcB primitive.RegionType
	Dst  primitive.RegionType
	Op   OperationType
	Mode ExecutionMode
}
