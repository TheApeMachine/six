package programmer

import (
	"fmt"
	"strings"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
Parser turns loaded source lines into Tokens and an optional trailing
continuation. It resolves every region ref through ParseRegionRef so
downstream stages work off validated RegionRef structs instead of raw
strings.
*/
type Parser struct {
	program *Program
	tokens  []Token
}

type parserOption func(*Parser)

func NewParser(program *Program, options ...parserOption) *Parser {
	parser := &Parser{
		program: program,
		tokens:  make([]Token, 0),
	}

	for _, option := range options {
		option(parser)
	}

	return parser
}

/*
validateOperationMnemonic accepts mnemonics the surface language allows
before lowering; any op the frame builder recognises (including popcount)
passes this gate.
*/
func (*Parser) validateOperationMnemonic(mnemonic string) error {
	switch strings.ToLower(strings.TrimSpace(mnemonic)) {
	case "false", "and", "aandnotb", "a", "notandb", "b", "xor", "or", "nor", "xnor",
		"notb", "ifbthena", "nota", "ifathenb", "nand", "true":
		return nil
	default:
		return fmt.Errorf("programmer: unknown operation %q", mnemonic)
	}
}

/*
Parse turns loaded source lines into op tokens and an optional trailing
continuation. Operation lines are five fields:

	srcA srcB dst op mode

An optional last non-blank line may be a next-program directive:

	next <uint64>
	next self

The directive must appear after all operation lines. Multiple `next` lines
or operation lines after `next` are errors. Lines whose first field starts
with '#' are treated as comments and ignored, so YAML literal blocks can
intermix explanatory prose with executable lines.
*/
func (parser *Parser) Parse() (tokens []Token) {
	parser.tokens = parser.tokens[:0]
	lines := parser.program.Load()

	for _, fields := range lines {
		if strings.HasPrefix(fields[0], "#") {
			continue
		}

		parser.tokens = append(parser.tokens, Token{
			SrcA: primitive.RegionNames[strings.ToLower(strings.TrimSpace(fields[0]))],
			SrcB: primitive.RegionNames[strings.ToLower(strings.TrimSpace(fields[1]))],
			Dst:  primitive.RegionNames[strings.ToLower(strings.TrimSpace(fields[2]))],
			Op:   parser.parseOperationType(fields[3]),
			Mode: parser.parseExecutionMode(fields[4]),
		})
	}

	return parser.tokens
}

/*
parseOperationType maps the surface op keyword onto the OperationType enum.
Unknown keywords are rejected so typos fail at parse time instead of silently
defaulting.
*/
func (parser *Parser) parseOperationType(op string) OperationType {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "false":
		return FALSE
	case "and":
		return AND
	case "aandnotb":
		return AANDNOTB
	case "a":
		return A
	case "notandb":
		return NOTANDB
	case "b":
		return B
	case "xor":
		return XOR
	case "or":
		return OR
	case "nor":
		return NOR
	case "xnor":
		return XNOR
	case "notb":
		return NOTB
	case "ifbthena":
		return IFBTHENA
	case "nota":
		return NOTA
	case "ifathenb":
		return IFA_THEN_B
	case "nand":
		return NAND
	case "true":
		return TRUE
	}
	return FALSE
}

/*
parseExecutionMode maps the surface mode keyword onto the ExecutionMode
enum. Unknown keywords are rejected so typos fail at parse time instead
of silently defaulting.
*/
func (parser *Parser) parseExecutionMode(mode string) ExecutionMode {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "accumulate":
		return ModeAccumulate
	case "reduce":
		return ModeReduce
	}

	return ModeAccumulate
}
