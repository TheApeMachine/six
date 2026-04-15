package programmer

import (
	"fmt"
	"strconv"
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
	program      *Program
	tokens       []Token
	continuation *Continuation
	err          error
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
		"notb", "ifbthena", "nota", "ifathenb", "nand", "true",
		"compose", "sandwich", "reverse":
		return nil
	default:
		return fmt.Errorf("programmer: unknown operation %q", mnemonic)
	}
}

type Continuation struct {
	ValueID uint64
	Self    bool
}

/*
Err returns the first parse error. Parse keeps the existing one-return API so
callers that only need tokens stay simple.
*/
func (parser *Parser) Err() error {
	if parser == nil {
		return nil
	}

	return parser.err
}

/*
Continuation returns the trailing scheduler directive captured during Parse.
Nil means the source omitted a next-program line.
*/
func (parser *Parser) Continuation() *Continuation {
	if parser == nil {
		return nil
	}

	return parser.continuation
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
	parser.continuation = nil
	parser.err = nil

	lines := parser.program.Load()
	sawContinuation := false

	for _, fields := range lines {
		if len(fields) == 0 {
			continue
		}

		if strings.HasPrefix(fields[0], "#") {
			continue
		}

		if strings.EqualFold(strings.TrimSpace(fields[0]), "next") {
			if sawContinuation {
				parser.err = fmt.Errorf("programmer: multiple next directives")
				return parser.tokens
			}

			if len(fields) != 2 {
				parser.err = fmt.Errorf("programmer: malformed next directive")
				return parser.tokens
			}

			sawContinuation = true
			parser.continuation = &Continuation{}

			if strings.EqualFold(strings.TrimSpace(fields[1]), "self") {
				parser.continuation.Self = true
				continue
			}

			valueID, err := strconv.ParseUint(strings.TrimSpace(fields[1]), 10, 64)
			if err != nil {
				parser.err = fmt.Errorf("programmer: invalid next target %q", fields[1])
				return parser.tokens
			}

			parser.continuation.ValueID = valueID
			continue
		}

		if sawContinuation {
			parser.err = fmt.Errorf("programmer: operation after next directive")
			return parser.tokens
		}

		if len(fields) < 5 {
			parser.err = fmt.Errorf("programmer: expected five fields, got %d", len(fields))
			return parser.tokens
		}

		srcA, ok := parser.parseRegionRef(fields[0])
		if !ok {
			return parser.tokens
		}

		srcB, ok := parser.parseRegionRef(fields[1])
		if !ok {
			return parser.tokens
		}

		dst, ok := parser.parseRegionRef(fields[2])
		if !ok {
			return parser.tokens
		}

		parser.tokens = append(parser.tokens, Token{
			SrcA: srcA,
			SrcB: srcB,
			Dst:  dst,
			Op:   parser.parseOperationType(fields[3]),
			Mode: parser.parseExecutionMode(fields[4]),
		})
	}

	return parser.tokens
}

/*
parseRegionRef accepts bare region names, region[index], and
region[index,span]. Offsets are relative to that named region.
*/
func (parser *Parser) parseRegionRef(raw string) (RegionRef, bool) {
	text := strings.ToLower(strings.TrimSpace(raw))
	name := text
	offset := 0
	span := -1

	if open := strings.IndexByte(text, '['); open >= 0 {
		if !strings.HasSuffix(text, "]") {
			parser.err = fmt.Errorf("programmer: malformed region ref %q", raw)
			return RegionRef{}, false
		}

		name = strings.TrimSpace(text[:open])
		body := strings.TrimSpace(text[open+1 : len(text)-1])
		left, right, hasSpan := strings.Cut(body, ",")

		parsedOffset, err := strconv.Atoi(strings.TrimSpace(left))
		if err != nil || parsedOffset < 0 {
			parser.err = fmt.Errorf("programmer: invalid region offset %q", raw)
			return RegionRef{}, false
		}

		offset = parsedOffset
		span = 1

		if hasSpan {
			parsedSpan, err := strconv.Atoi(strings.TrimSpace(right))
			if err != nil || parsedSpan <= 0 {
				parser.err = fmt.Errorf("programmer: invalid region span %q", raw)
				return RegionRef{}, false
			}

			span = parsedSpan
		}
	}

	region, ok := primitive.RegionNames[name]
	if !ok {
		parser.err = fmt.Errorf("programmer: unknown region %q", raw)
		return RegionRef{}, false
	}

	start, words := region.WordExtent()
	if span < 0 {
		span = words
	}

	if offset+span > words {
		parser.err = fmt.Errorf("programmer: region ref %q exceeds %d words", raw, words)
		return RegionRef{}, false
	}

	return RegionRef{
		Region: region,
		Start:  start + offset,
		Span:   span,
	}, true
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
	case "compose":
		return COMPOSE
	case "sandwich":
		return SANDWICH
	case "reverse":
		return REVERSE
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
