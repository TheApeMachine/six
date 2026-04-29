package program

import "github.com/theapemachine/six/pkg/compute/compiler"

const (
	OpFalse    = compiler.OpFalse
	OpAnd      = compiler.OpAnd
	OpAandNotB = compiler.OpAandNotB
	OpCopyA    = compiler.OpCopyA
	OpNotAandB = compiler.OpNotAandB
	OpCopyB    = compiler.OpCopyB
	OpXor      = compiler.OpXor
	OpOr       = compiler.OpOr
	OpNor      = compiler.OpNor
	OpXnor     = compiler.OpXnor
	OpNotB     = compiler.OpNotB
	OpIfBthenA = compiler.OpIfBthenA
	OpNotA     = compiler.OpNotA
	OpIfAthenB = compiler.OpIfAthenB
	OpNand     = compiler.OpNand
	OpTrue     = compiler.OpTrue
)

const (
	TopoLocal     = compiler.TopoLocal
	TopoPopQueue  = compiler.TopoPopQueue
	TopoHypercube = compiler.TopoHypercube
)

const (
	OpReduceArgMinNonZero = compiler.OpReduceArgMinNonZero
	OpReduceModeEq        = compiler.OpReduceModeEq
	OpReduceZipfSelect    = compiler.OpReduceZipfSelect
)

const (
	PredLT          = compiler.PredLT
	PredLE          = compiler.PredLE
	PredGT          = compiler.PredGT
	PredGE          = compiler.PredGE
	PredEQ          = compiler.PredEQ
	PredNE          = compiler.PredNE
	PredStorePopcnt = compiler.PredStorePopcnt
	PredAnyZero     = compiler.PredAnyZero
)

/*
SlotIR is one resident program slot.
Empty slots are encoded as zero words and still appear in disassembly.
*/
type SlotIR struct {
	Empty bool
	Op    MachineOp
}

/*
ProgramIR is the canonical authoring artifact for resident firmware.
It is already a machine-level sweep: frontends must resolve names, regions,
properties, and constants before they hand it to this package.
*/
type ProgramIR struct {
	Name      string
	Slots     []SlotIR
	Constants []ConstantInit
}

/*
MachineOp is the substrate-neutral packed instruction in field form.
Packing MachineOp is the final step before the words are copied into Value
program memory.
*/
type MachineOp struct {
	AStart        uint64
	ASpan         uint64
	BStart        uint64
	BSpan         uint64
	DstStart      uint64
	DstSpan       uint64
	MaskStart     uint64
	Opcode        uint64
	TargetB       bool
	Emit          bool
	TargetChild   bool // pack destination as target-C so writes land on the emitted child frame
	Topology      uint64
	Predicate     bool
	PredicateCond uint64
	SrcAFromB     bool
	Stage         bool
	PopEnd        bool
}
