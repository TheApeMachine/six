package programmer

import (
	"fmt"
	"strconv"
	"strings"
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
	return newFrameBuilder(nil).acceptSourceOp(mnemonic)
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
func (parser *Parser) Parse() (tokens []Token, trailing *Continuation, err error) {
	parser.tokens = parser.tokens[:0]

	var continuation *Continuation

	lines := parser.program.Load()

	for rowIndex, fields := range lines {
		if strings.HasPrefix(fields[0], "#") {
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

				continue
			}

			valueID, parseErr := strconv.ParseUint(fields[1], 10, 64)

			if parseErr != nil {
				return nil, nil, fmt.Errorf("line %d: invalid next value ID: %w", rowIndex+1, parseErr)
			}

			continuation = &Continuation{Kind: ContinuationValueID, ValueID: valueID}

			continue
		}

		if continuation != nil {
			return nil, nil, fmt.Errorf("line %d: operation after next directive", rowIndex+1)
		}

		if len(fields) != 5 {
			return nil, nil, fmt.Errorf("line %d: want 5 fields (srcA srcB dst op mode), got %d: %v",
				rowIndex+1, len(fields), fields)
		}

		srcARef, refErr := ParseRegionRef(fields[0])

		if refErr != nil {
			return nil, nil, fmt.Errorf("line %d: srcA: %w", rowIndex+1, refErr)
		}

		srcBRef, refErr := ParseRegionRef(fields[1])

		if refErr != nil {
			return nil, nil, fmt.Errorf("line %d: srcB: %w", rowIndex+1, refErr)
		}

		dstRef, refErr := ParseRegionRef(fields[2])

		if refErr != nil {
			return nil, nil, fmt.Errorf("line %d: dst: %w", rowIndex+1, refErr)
		}

		if opErr := parser.validateOperationMnemonic(fields[3]); opErr != nil {
			return nil, nil, fmt.Errorf("line %d: unknown op %q", rowIndex+1, fields[3])
		}

		modeBit, modeErr := parseExecutionMode(fields[4])

		if modeErr != nil {
			return nil, nil, fmt.Errorf("line %d: %w", rowIndex+1, modeErr)
		}

		parser.tokens = append(parser.tokens, Token{
			SrcA:    fields[0],
			SrcB:    fields[1],
			Dst:     fields[2],
			Op:      fields[3],
			Mode:    fields[4],
			SrcARef: srcARef,
			SrcBRef: srcBRef,
			DstRef:  dstRef,
			ModeBit: modeBit,
		})
	}

	return parser.tokens, continuation, nil
}

/*
parseExecutionMode maps the surface mode keyword onto the ExecutionMode
enum. Unknown keywords are rejected so typos fail at parse time instead
of silently defaulting.
*/
func parseExecutionMode(mode string) (ExecutionMode, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "accumulate":
		return ModeAccumulate, nil
	case "reduce":
		return ModeReduce, nil
	}

	return 0, fmt.Errorf("unknown execution mode %q", mode)
}
