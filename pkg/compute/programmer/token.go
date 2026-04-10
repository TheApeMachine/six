package programmer

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

type Token struct {
	a   RegionType
	b   RegionType
	dst RegionType
	op  OperationType
}
