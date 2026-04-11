package programmer

import (
	"fmt"
	"strings"

	"github.com/theapemachine/six/pkg/compute/kernel"
)

type CompilerTarget uint8

const (
	CPU CompilerTarget = iota
	Metal
	CUDA
)

/*
Compiler lowers a tokenized program into one or more frames. Each frame is
sized to fit a single Value program region; when the source does not fit,
compilation produces multiple frames and the stage emits multiple Values.

Optional continuation (trailing `next` line) is stored for Executable to apply
after Values are minted (word 117, plus implicit chaining across a batch).
*/
type Compiler struct {
	tokens       []Token
	continuation *Continuation
}

type compilerOption func(*Compiler)

func NewCompiler(tokens []Token, opts ...compilerOption) *Compiler {
	compiler := &Compiler{tokens: tokens}

	for _, option := range opts {
		option(compiler)
	}

	return compiler
}

/*
Tokens returns the compiler’s source token slice (same order as emitted frames).
*/
func (compiler *Compiler) Tokens() []Token {
	return compiler.tokens
}

/*
WithContinuation attaches a parsed trailing `next` directive. Typically taken
from Parser.Parse alongside the token slice.
*/
func WithContinuation(cont *Continuation) compilerOption {
	return func(compiler *Compiler) {
		compiler.continuation = cont
	}
}

/*
Continuation returns the optional trailing next-program directive (may be nil).
*/
func (compiler *Compiler) Continuation() *Continuation {
	return compiler.continuation
}

/*
Compile the tokens into a slice of contiguous frames, optimized for the given target.
*/
func (compiler *Compiler) Compile(target CompilerTarget) ([]Frame, error) {
	switch target {
	case CPU:
		return NewCPUCompiler(compiler.tokens).Compile()
	case Metal:
		return NewMetalCompiler(compiler.tokens).Compile()
	case CUDA:
		return NewCUDACompiler(compiler.tokens).Compile()
	}

	return nil, fmt.Errorf("unsupported target: %d", target)
}

/*
frameBuilder lowers token lines into program-region Frames. Tokens may be empty
when used only for mnemonic validation during parse.
*/
type frameBuilder struct {
	tokens []Token
}

func newFrameBuilder(tokens []Token) *frameBuilder {
	return &frameBuilder{tokens: tokens}
}

/*
frames emits one Frame per token for the chosen substrate target.
*/
func (builder *frameBuilder) frames(target CompilerTarget) ([]Frame, error) {
	out := make([]Frame, 0, len(builder.tokens))

	for _, tok := range builder.tokens {
		op, err := builder.truthTableFromMnemonic(tok.Op)

		if err != nil {
			return nil, err
		}

		var frame Frame

		builder.packTruth(&frame, op, tok, target)
		out = append(out, frame)
	}

	return out, nil
}

/*
acceptSourceOp allows surface ops that are not yet lowered (e.g. popcount) plus
truth-table mnemonics.
*/
func (builder *frameBuilder) acceptSourceOp(mnemonic string) error {
	key := strings.ToLower(strings.TrimSpace(mnemonic))

	if key == "popcount" {
		return nil
	}

	_, err := builder.truthTableFromMnemonic(key)

	return err
}

/*
truthTableFromMnemonic maps surface mnemonics to the 4-bit truth-table space.
*/
func (*frameBuilder) truthTableFromMnemonic(mnemonic string) (OperationType, error) {
	key := strings.ToLower(strings.TrimSpace(mnemonic))

	switch key {
	case "false":
		return FALSE, nil
	case "and":
		return AND, nil
	case "aandnotb":
		return AANDNOTB, nil
	case "a":
		return A, nil
	case "notandb":
		return NOTANDB, nil
	case "b":
		return B, nil
	case "xor":
		return XOR, nil
	case "or":
		return OR, nil
	case "nor":
		return NOR, nil
	case "xnor":
		return XNOR, nil
	case "notb":
		return NOTB, nil
	case "ifbthena":
		return IFBTHENA, nil
	case "nota":
		return NOTA, nil
	case "ifathenb":
		return IFA_THEN_B, nil
	case "nand":
		return NAND, nil
	case "true":
		return TRUE, nil
	default:
		return 0, fmt.Errorf("programmer: unknown or unsupported op %q", mnemonic)
	}
}

/*
packTruth fills the program region words universalBitwiseV2 reads at run
time. The wire layout matches kernel/layout.go's Program*Word constants:

	[0] legacy opcode byte in the low 8 bits (geometric / xor dispatch gate)
	[1] 16-nibble rotation opcode table
	[2] ExecutionMode (0 accumulate, 1 reduce)
	[3] srcA absolute start / span
	[4] srcB absolute start / span
	[5] dst  absolute start / span

packTruth does not touch operand region data — those words stay whatever
the input Value held when the frame is stamped onto it. The substrate
walks the packed refs on its own and reads / writes the Value in place.
*/
func (*frameBuilder) packTruth(
	frame *Frame,
	op OperationType,
	tok Token,
	target CompilerTarget,
) {
	nibble := uint64(op) & 0xF

	frame.Program[0] = nibble

	var table uint64

	for rotation := 0; rotation < 16; rotation++ {
		table |= nibble << (rotation * 4)
	}

	frame.Program[1] = table
	frame.Program[2] = uint64(tok.ModeBit)
	frame.Program[3] = kernel.PackRegionRef(tok.SrcARef.AbsStart(), tok.SrcARef.Span)
	frame.Program[4] = kernel.PackRegionRef(tok.SrcBRef.AbsStart(), tok.SrcBRef.Span)
	frame.Program[5] = kernel.PackRegionRef(tok.DstRef.AbsStart(), tok.DstRef.Span)

	_ = target
}
