package programmer

import (
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
)

type Builder struct {
	tokens []Token
}

func NewBuilder(tokens []Token) *Builder {
	return &Builder{tokens: tokens}
}

func (builder *Builder) contractFor(tok Token) FrameContract {
	if tok.Op == COMPOSE || tok.Op == SANDWICH || tok.Op == REVERSE {
		return ContractGeometric
	}

	if tok.Mode == ModeReduce {
		return ContractReduceBinary
	}

	if (tok.Dst.Region == primitive.PrevRegion || tok.Dst.Region == primitive.NextRegion) && tok.Dst.Span == 1 {
		return ContractExactBinary
	}

	if tok.Dst.Region == primitive.PropertiesRegion && tok.Dst.Span <= 2 {
		return ContractExactBinary
	}

	return ContractSweepSignal
}

func (builder *Builder) build(target CompilerTarget) ([]Frame, error) {
	_ = target
	out := make([]Frame, 0, len(builder.tokens))

	for _, tok := range builder.tokens {
		frame := Frame{Contract: builder.contractFor(tok)}
		builder.packTruth(&frame, tok.Op, tok)
		out = append(out, frame)
	}

	return out, nil
}

/**
encodeMode packs the FrameContract and ExecutionMode into one uint64 program
word. The low byte stores the ExecutionMode value, while the next byte stores
one of kernel.ProgramContractUnknown, kernel.ProgramContractExactBinary,
kernel.ProgramContractSweepSignal, kernel.ProgramContractReduce, or
kernel.ProgramContractGeometric shifted left by kernel.ProgramContractShift.
This keeps the lowered FrameContract and ExecutionMode together in
kernel.ProgramModeWord so kernels can decode both pieces of intent from one
word.
*/
func (builder *Builder) encodeMode(contract FrameContract, mode ExecutionMode) uint64 {
	encodedContract := kernel.ProgramContractUnknown

	switch contract {
	case ContractExactBinary:
		encodedContract = kernel.ProgramContractExactBinary
	case ContractSweepSignal:
		encodedContract = kernel.ProgramContractSweepSignal
	case ContractReduceBinary:
		encodedContract = kernel.ProgramContractReduce
	case ContractGeometric:
		encodedContract = kernel.ProgramContractGeometric
	}

	return uint64(mode) | (encodedContract << kernel.ProgramContractShift)
}

func (builder *Builder) packTruth(frame *Frame, op OperationType, tok Token) {
	if frame.Contract == ContractGeometric {
		frame.Program[0] = uint64(op)
		frame.Program[1] = 0
		frame.Program[2] = builder.encodeMode(frame.Contract, tok.Mode)
		frame.Program[3] = kernel.PackRegionRef(tok.SrcA.WordExtent())
		frame.Program[4] = kernel.PackRegionRef(tok.SrcB.WordExtent())
		frame.Program[5] = kernel.PackRegionRef(tok.Dst.WordExtent())
		return
	}

	nibble := uint64(op) & 0xF

	frame.Program[0] = nibble

	var table uint64

	for rotation := 0; rotation < 16; rotation++ {
		table |= nibble << (rotation * 4)
	}

	frame.Program[1] = table
	frame.Program[2] = builder.encodeMode(frame.Contract, tok.Mode)
	frame.Program[3] = kernel.PackRegionRef(tok.SrcA.WordExtent())
	frame.Program[4] = kernel.PackRegionRef(tok.SrcB.WordExtent())
	frame.Program[5] = kernel.PackRegionRef(tok.Dst.WordExtent())
}
