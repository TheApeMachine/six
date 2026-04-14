package programmer

import "github.com/theapemachine/six/pkg/compute/kernel"

type Builder struct {
	tokens []Token
}

func NewBuilder(tokens []Token) *Builder {
	return &Builder{tokens: tokens}
}

func (builder *Builder) build(target CompilerTarget) ([]Frame, error) {
	_ = target
	out := make([]Frame, 0, len(builder.tokens))

	for _, tok := range builder.tokens {
		frame := Frame{}
		builder.packTruth(&frame, tok.Op, tok)
		out = append(out, frame)
	}

	return out, nil
}

func (builder *Builder) packTruth(frame *Frame, op OperationType, tok Token) {
	nibble := uint64(op) & 0xF

	frame.Program[0] = nibble

	var table uint64

	for rotation := 0; rotation < 16; rotation++ {
		table |= nibble << (rotation * 4)
	}

	frame.Program[1] = table
	frame.Program[2] = uint64(tok.Mode)
	frame.Program[3] = kernel.PackRegionRef(tok.SrcA.WordExtent())
	frame.Program[4] = kernel.PackRegionRef(tok.SrcB.WordExtent())
	frame.Program[5] = kernel.PackRegionRef(tok.Dst.WordExtent())
}
