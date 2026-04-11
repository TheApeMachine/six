package programmer

import (
	"fmt"
	"regexp"
	"strconv"
)

const (
	regionRefPattern = `^[a-z]+\[[0-9,]+]$`
)

var regionRefRE = regexp.MustCompile(regionRefPattern)

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
validateOperationMnemonic accepts mnemonics the surface language allows before lowering.
*/
func (*Parser) validateOperationMnemonic(mnemonic string) error {
	return newFrameBuilder(nil).acceptSourceOp(mnemonic)
}

/*
Parse turns loaded source lines into op tokens and an optional trailing
continuation. Operation lines are five fields:

	srcA srcB dst op mode

An optional last non-blank line may be a next-program directive:

	next <uint64>
	next self

The directive must appear after all operation lines. Multiple `next` lines or
operation lines after `next` are errors.
*/
func (parser *Parser) Parse() (tokens []Token, trailing *Continuation, err error) {
	parser.tokens = parser.tokens[:0]

	var continuation *Continuation
	lines := parser.program.Load()

	for rowIndex, fields := range lines {
		if len(fields) == 0 {
			continue
		}

		if fields[0] == "next" {
			if continuation != nil {
				return nil, nil, fmt.Errorf("line %d: duplicate next directive", rowIndex+1)
			}

			if len(fields) < 2 {
				return nil, nil, fmt.Errorf("line %d: next requires a target", rowIndex+1)
			}

			if fields[1] == "self" {
				continuation = &Continuation{Kind: ContinuationSelf}
			} else {
				valueID, err := strconv.ParseUint(fields[1], 10, 64)

				if err != nil {
					return nil, nil, fmt.Errorf("line %d: invalid next value ID: %w", rowIndex+1, err)
				}

				continuation = &Continuation{Kind: ContinuationValueID, ValueID: valueID}
			}

			continue
		} else if continuation != nil {
			return nil, nil, fmt.Errorf("line %d: operation after next directive", rowIndex+1)
		}

		if len(fields) != 5 {
			return nil, nil, fmt.Errorf("line %d: want 5 fields (srcA srcB dst op mode), got %d: %v",
				rowIndex+1, len(fields), fields)
		}

		for col, label := range []string{"srcA", "srcB", "dst"} {
			if !regionRefRE.MatchString(fields[col]) {
				return nil, nil, fmt.Errorf("line %d: invalid %s %q", rowIndex+1, label, fields[col])
			}
		}

		if err := parser.validateOperationMnemonic(fields[3]); err != nil {
			return nil, nil, fmt.Errorf("line %d: unknown op %q", rowIndex+1, fields[3])
		}

		parser.tokens = append(parser.tokens, Token{
			SrcA: fields[0],
			SrcB: fields[1],
			Dst:  fields[2],
			Op:   fields[3],
			Mode: fields[4],
		})
	}

	return parser.tokens, continuation, nil
}
