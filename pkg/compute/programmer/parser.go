package programmer

import (
	"fmt"
	"strconv"
	"strings"
)

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

	return &Parser{program: program}
}

/*
Parse a program string into a tokenized representation.
*/
func (parser *Parser) Parse() ([]Token, error) {
	for line := range strings.SplitSeq(parser.program.source, "\n") {
		fields := strings.Fields(line)

		if len(fields) != 3 {
			return nil, fmt.Errorf("invalid line: %s", line)
		}

		op, err := strconv.ParseUint(fields[0], 2, 4)

		if err != nil {
			return nil, fmt.Errorf("invalid opcode: %s", fields[0])
		}

		a, err := strconv.ParseUint(fields[1], 2, 4)

		if err != nil {
			return nil, fmt.Errorf("invalid a: %s", fields[1])
		}

		b, err := strconv.ParseUint(fields[2], 2, 4)

		if err != nil {
			return nil, fmt.Errorf("invalid b: %s", fields[2])
		}

		dst, err := strconv.ParseUint(fields[3], 2, 4)

		if err != nil {
			return nil, fmt.Errorf("invalid dst: %s", fields[3])
		}

		parser.tokens = append(parser.tokens, Token{
			a:   RegionType(a),
			b:   RegionType(b),
			dst: RegionType(dst),
			op:  OperationType(op),
		})
	}

	return parser.tokens, nil
}
