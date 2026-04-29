package compiler

import (
	"fmt"
	"strconv"
	"unicode"
)

// Compile lowers a single program block into the 16-word ALU image plus
// the constant initializers the host must stage in the asset region.
func Compile(source string) (Compiled, error) {
	tokens, err := tokenize(source)
	if err != nil {
		return Compiled{}, err
	}

	parser := &parser{tokens: tokens, builder: NewBuilder()}

	block, err := parser.parseProgram()
	if err != nil {
		return Compiled{}, err
	}

	block.Walk(parser.builder)

	if overflow := parser.builder.Overflow(); overflow > 0 {
		return Compiled{}, fmt.Errorf("compiler: program exceeds 16-instruction budget by %d", overflow)
	}

	return Compiled{
		Words:         parser.builder.Compile(),
		Constants:     parser.builder.Constants(),
		Substitutions: parser.builder.Substitutions(),
		MaskTrueWord:  MaskTrue.Start,
	}, nil
}

type token struct {
	kind string
	text string
}

func tokenize(source string) ([]token, error) {
	var out []token
	cursor := 0

	for cursor < len(source) {
		char := source[cursor]

		switch {
		case char == ';':
			for cursor < len(source) && source[cursor] != '\n' {
				cursor++
			}
		case unicode.IsSpace(rune(char)):
			cursor++
		case char == '{' && cursor+1 < len(source) && source[cursor+1] == '{':
			out = append(out, token{"{{", "{{"})
			cursor += 2
		case char == '}' && cursor+1 < len(source) && source[cursor+1] == '}':
			out = append(out, token{"}}", "}}"})
			cursor += 2
		case char == '(' || char == ')' || char == '{' || char == '}' || char == '.' || char == ',' || char == '[' || char == ']':
			out = append(out, token{string(char), string(char)})
			cursor++
		case char == '<' && cursor+1 < len(source) && source[cursor+1] == '-':
			out = append(out, token{"<-", "<-"})
			cursor += 2
		case char == '<' && cursor+1 < len(source) && source[cursor+1] == '=':
			out = append(out, token{"<=", "<="})
			cursor += 2
		case char == '>' && cursor+1 < len(source) && source[cursor+1] == '=':
			out = append(out, token{">=", ">="})
			cursor += 2
		case char == '=' && cursor+1 < len(source) && source[cursor+1] == '=':
			out = append(out, token{"==", "=="})
			cursor += 2
		case char == '!' && cursor+1 < len(source) && source[cursor+1] == '=':
			out = append(out, token{"!=", "!="})
			cursor += 2
		case char == '>' || char == '<':
			out = append(out, token{string(char), string(char)})
			cursor++
		case unicode.IsDigit(rune(char)):
			end := cursor
			for end < len(source) && unicode.IsDigit(rune(source[end])) {
				end++
			}
			out = append(out, token{"num", source[cursor:end]})
			cursor = end
		case isIdentStart(rune(char)):
			end := cursor
			for end < len(source) && isIdent(rune(source[end])) {
				end++
			}
			out = append(out, token{"ident", source[cursor:end]})
			cursor = end
		default:
			return nil, fmt.Errorf("compiler: unexpected character %q at offset %d", char, cursor)
		}
	}

	return out, nil
}

func isIdentStart(char rune) bool { return unicode.IsLetter(char) || char == '_' }
func isIdent(char rune) bool {
	return unicode.IsLetter(char) || unicode.IsDigit(char) || char == '_'
}

/*
parser is a tiny hand-rolled descender that turns the program dialect
into an AST that drives the Builder.
*/
type parser struct {
	tokens  []token
	pos     int
	builder *Builder
	// emitDepth makes bare destinations inside emit blocks resolve to
	// the emitted frame rather than the resident A frame.
	emitDepth int
	// lastBinop is the parser's scratch slot for the second operand of
	// a binary truth-table call. parseRHS hands a single sideRegion
	// back, so the second operand rides this side-channel until
	// makeAssignFromRHS picks it up.
	lastBinop *binop
}

func (parser *parser) peek() token {
	if parser.pos >= len(parser.tokens) {
		return token{kind: "eof"}
	}

	return parser.tokens[parser.pos]
}

func (parser *parser) lookahead(offset int) token {
	if parser.pos+offset >= len(parser.tokens) {
		return token{kind: "eof"}
	}

	return parser.tokens[parser.pos+offset]
}

func (parser *parser) advance() token {
	tok := parser.peek()
	parser.pos++

	return tok
}

func (parser *parser) expect(kind string) (token, error) {
	tok := parser.advance()

	if tok.kind != kind {
		return tok, fmt.Errorf("compiler: expected %q, got %q (text %q)", kind, tok.kind, tok.text)
	}

	return tok, nil
}

/*
sideRegion pairs a physical region with the operand pointer side that
reads it ('A' = owner, 'B' = popped). For RHS expressions that lower
into a predicate-store (popcnt / any_zero), Predicate is set so the
caller can emit it in place of a regular AssignNode.
*/
type sideRegion struct {
	side   byte
	reg    Region
	rotate uint64
	// predicate set when the RHS is a popcnt/any-zero reduction; if so
	// the destination receives a folded scalar from the popcount engine
	// instead of a bitwise truth-table op.
	predicate     bool
	predicateOp   uint64
	predicateSrc  Region
	predicateSide byte
	reduce        bool
	reduceOp      uint64
	reduceValue   Region
	reduceKey     Region
	reduceMatch   Region
	// literalZero distinguishes `set X <- 0` (which lowers to OpFalse
	// across the full destination span) from a generic single-word
	// constant allocation that happens to hold zero.
	literalZero bool
	// indirect marks operands written as `{{expr}}` in the source. The
	// reg field then holds the INDIRECTION ADDRESS (the word the host
	// will read at install time to resolve the actual operand). The
	// builder records a substitution slot so InstallFirmware can patch
	// the packed instruction word per Value before the kernel sees it.
	indirect bool
}

/*
predicateSource is the left side of an if-comparison. Simple sources point
directly at a region. Expression sources carry a short prelude that materializes
their truth-table result into scratch asset words before the predicate folds it.
*/
type predicateSource struct {
	side         byte
	reg          Region
	rotate       uint64
	prelude      BlockNode
	indirectAddr uint64
}

func (source predicateSource) sideRegion() sideRegion {
	return sideRegion{
		side:     source.side,
		reg:      source.reg,
		rotate:   source.rotate,
		indirect: source.indirectAddr != 0,
	}
}

func (parser *parser) parseProgram() (BlockNode, error) {
	if tok := parser.advance(); tok.kind != "ident" || tok.text != "program" {
		return BlockNode{}, fmt.Errorf("compiler: expected `program` keyword, got %q", tok.text)
	}

	if _, err := parser.expect("ident"); err != nil {
		return BlockNode{}, err
	}

	if _, err := parser.expect("{"); err != nil {
		return BlockNode{}, err
	}

	body, err := parser.parseStatements("}")
	if err != nil {
		return BlockNode{}, err
	}

	if _, err := parser.expect("}"); err != nil {
		return BlockNode{}, err
	}

	return body, nil
}

func (parser *parser) parseStatements(terminator string) (BlockNode, error) {
	var stmts []ASTNode

	for parser.peek().kind != terminator && parser.peek().kind != "eof" {
		node, err := parser.parseStatement()
		if err != nil {
			return BlockNode{}, err
		}

		if node != nil {
			stmts = append(stmts, node)
		}
	}

	return BlockNode{Statements: stmts}, nil
}

func (parser *parser) parseStatement() (ASTNode, error) {
	tok := parser.peek()

	if tok.kind == "(" {
		return parser.parseIf()
	}

	if tok.kind == "ident" {
		switch tok.text {
		case "set", "write":
			return parser.parseAssign()
		case "pop":
			return parser.parsePop()
		case "gossip":
			return parser.parseGossip()
		case "stage":
			return parser.parseStage()
		case "emit":
			return parser.parseEmit()
		case "geometric":
			return parser.parseGeometric()
		}
	}

	return nil, fmt.Errorf("compiler: unexpected token %q (%q) at pos %d", tok.kind, tok.text, parser.pos)
}

func (parser *parser) parseGeometric() (ASTNode, error) {
	if _, err := parser.expect("ident"); err != nil {
		return nil, err
	}

	opTok, err := parser.expect("ident")
	if err != nil {
		return nil, err
	}

	op, ok := geometricOpcodeOf(opTok.text)
	if !ok {
		return nil, fmt.Errorf("compiler: unknown geometric op %q", opTok.text)
	}

	return GeometricNode{Op: op}, nil
}

func (parser *parser) parseIf() (ASTNode, error) {
	if _, err := parser.expect("("); err != nil {
		return nil, err
	}

	cond, region, threshold, err := parser.parseComparison()
	if err != nil {
		return nil, err
	}

	if threshold.side == 'B' {
		return nil, fmt.Errorf("compiler: B-side predicate thresholds are not supported")
	}

	if _, err := parser.expect(")"); err != nil {
		return nil, err
	}

	mask := parser.builder.AllocConstant(0)

	if _, err := parser.expect("{"); err != nil {
		return nil, err
	}

	body, err := parser.parseStatements("}")
	if err != nil {
		return nil, err
	}

	if _, err := parser.expect("}"); err != nil {
		return nil, err
	}

	return IfNode{
		Cond:                  cond,
		Prelude:               region.prelude,
		Region:                region.reg,
		RegionSide:            region.side,
		RegionIndirectAddr:    region.indirectAddr,
		Threshold:             threshold.reg,
		ThresholdIndirectAddr: indirectAddrOf(threshold),
		Mask:                  mask,
		Body:                  body,
	}, nil
}

/*
indirectAddrOf returns the indirection address recorded on a sideRegion,
or 0 when the region is direct. Centralized so the parser does not have
to peek at the .indirect bit and the .reg.Start field together at every
construction site.
*/
func indirectAddrOf(region sideRegion) uint64 {
	if !region.indirect {
		return 0
	}

	return region.reg.Start
}

func (parser *parser) parseComparison() (cond uint64, region predicateSource, threshold sideRegion, err error) {
	lhs, err := parser.parsePredicateSource()
	if err != nil {
		return 0, predicateSource{}, sideRegion{}, err
	}

	cmpTok := parser.advance()

	cond, ok := condOf(cmpTok.text)
	if !ok {
		return 0, predicateSource{}, sideRegion{}, fmt.Errorf("compiler: unknown comparison %q", cmpTok.text)
	}

	rhs, err := parser.parseRegion()
	if err != nil {
		return 0, predicateSource{}, sideRegion{}, err
	}

	lhs, err = parser.materializePredicateSource(lhs)
	if err != nil {
		return 0, predicateSource{}, sideRegion{}, err
	}

	if rhs.rotate != 0 {
		return 0, predicateSource{}, sideRegion{}, fmt.Errorf("compiler: rot8 cannot be used as a predicate threshold")
	}

	return cond, lhs, rhs, nil
}

func (parser *parser) parsePredicateSource() (predicateSource, error) {
	if parser.peek().kind == "ident" && parser.peek().text == "popcnt" && parser.lookahead(1).kind == "(" {
		parser.advance()

		if _, err := parser.expect("("); err != nil {
			return predicateSource{}, err
		}

		source, err := parser.parsePredicateExpr()
		if err != nil {
			return predicateSource{}, err
		}

		if _, err := parser.expect(")"); err != nil {
			return predicateSource{}, err
		}

		return parser.forcePopcntSource(source)
	}

	region, err := parser.parseRegion()
	if err != nil {
		return predicateSource{}, err
	}

	return predicateSource{
		side:         region.side,
		reg:          region.reg,
		rotate:       region.rotate,
		indirectAddr: indirectAddrOf(region),
	}, nil
}

func (parser *parser) parsePredicateExpr() (predicateSource, error) {
	if parser.peek().kind == "ident" && parser.peek().text == "rot8" {
		region, err := parser.parseRegion()
		if err != nil {
			return predicateSource{}, err
		}

		return predicateSource{side: region.side, reg: region.reg, rotate: region.rotate}, nil
	}

	if parser.peek().kind == "ident" && parser.lookahead(1).kind == "(" && isCallName(parser.peek().text) {
		return parser.parsePredicateCall()
	}

	region, err := parser.parseRegion()
	if err != nil {
		return predicateSource{}, err
	}

	return predicateSource{
		side:         region.side,
		reg:          region.reg,
		rotate:       region.rotate,
		indirectAddr: indirectAddrOf(region),
	}, nil
}

func (parser *parser) parsePredicateCall() (predicateSource, error) {
	opName := parser.advance().text
	opcode, ok := opcodeOf(opName)
	if !ok {
		return predicateSource{}, fmt.Errorf("compiler: unknown predicate expression %q", opName)
	}

	if _, err := parser.expect("("); err != nil {
		return predicateSource{}, err
	}

	lhs, err := parser.parsePredicateExpr()
	if err != nil {
		return predicateSource{}, err
	}

	if _, err := parser.expect(","); err != nil {
		return predicateSource{}, err
	}

	rhs, err := parser.parsePredicateExpr()
	if err != nil {
		return predicateSource{}, err
	}

	if _, err := parser.expect(")"); err != nil {
		return predicateSource{}, err
	}

	span := lhs.reg.Span
	if rhs.reg.Span > span {
		span = rhs.reg.Span
	}

	scratch := predicateSource{
		side:    'A',
		reg:     parser.builder.AllocScratch(span),
		prelude: BlockNode{Statements: append([]ASTNode{}, lhs.prelude.Statements...)},
	}

	scratch.prelude.Statements = append(scratch.prelude.Statements, rhs.prelude.Statements...)

	node, err := parser.makeBinAssign(sideRegion{side: 'A', reg: scratch.reg}, opcode, lhs.sideRegion(), rhs.sideRegion())
	if err != nil {
		return predicateSource{}, err
	}

	scratch.prelude.Statements = append(scratch.prelude.Statements, node)

	return scratch, nil
}

func (parser *parser) materializePredicateSource(source predicateSource) (predicateSource, error) {
	if source.rotate == 0 {
		return source, nil
	}

	scratch := parser.builder.AllocScratch(source.reg.Span)
	node, err := parser.makeCopyAssign(sideRegion{side: 'A', reg: scratch}, source.sideRegion())
	if err != nil {
		return predicateSource{}, err
	}

	source.prelude.Statements = append(source.prelude.Statements, node)

	return predicateSource{
		side:    'A',
		reg:     scratch,
		prelude: source.prelude,
	}, nil
}

func (parser *parser) forcePopcntSource(source predicateSource) (predicateSource, error) {
	var err error
	source, err = parser.materializePredicateSource(source)
	if err != nil {
		return predicateSource{}, err
	}

	scratch := parser.builder.AllocScratch(1)
	source.prelude.Statements = append(source.prelude.Statements, PredicateAssignNode{
		Dst:        scratch,
		Cond:       PredStorePopcnt,
		Region:     source.reg,
		RegionSide: source.side,
		Target:     0,
	})

	return predicateSource{
		side:    'A',
		reg:     scratch,
		prelude: source.prelude,
	}, nil
}

func condOf(op string) (uint64, bool) {
	switch op {
	case "<":
		return PredLT, true
	case "<=":
		return PredLE, true
	case ">":
		return PredGT, true
	case ">=":
		return PredGE, true
	case "==":
		return PredEQ, true
	case "!=":
		return PredNE, true
	}

	return 0, false
}

func (parser *parser) parseAssign() (ASTNode, error) {
	parser.advance() // consume `set` or `write`

	dst, err := parser.parseRegion()
	if err != nil {
		return nil, err
	}

	if _, err := parser.expect("<-"); err != nil {
		return nil, err
	}

	rhs, err := parser.parseRHS()
	if err != nil {
		return nil, err
	}

	if rhs.predicate {
		return PredicateAssignNode{
			Dst:        dst.reg,
			Cond:       rhs.predicateOp,
			Region:     rhs.predicateSrc,
			RegionSide: rhs.predicateSide,
			Target:     targetOf(dst.side),
		}, nil
	}

	if rhs.reduce {
		if dst.side != 'A' {
			return nil, fmt.Errorf("compiler: lane reducers must write to A-side destinations")
		}

		return ReduceAssignNode{
			Dst:   dst.reg,
			Op:    rhs.reduceOp,
			Value: rhs.reduceValue,
			Key:   rhs.reduceKey,
			Match: rhs.reduceMatch,
		}, nil
	}

	return parser.makeAssignFromRHS(dst, rhs)
}

func (parser *parser) parseRHS() (sideRegion, error) {
	tok := parser.peek()

	if tok.kind == "ident" && tok.text == "rot8" {
		return parser.parseRegion()
	}

	if tok.kind == "ident" && parser.lookahead(1).kind == "(" && isCallName(tok.text) {
		return parser.parseRHSCall()
	}

	return parser.parseRegion()
}

func (parser *parser) parseRHSCall() (sideRegion, error) {
	opName := parser.advance().text
	if _, err := parser.expect("("); err != nil {
		return sideRegion{}, err
	}

	// Single-argument reductions emit predicate-store or any-zero.
	if opName == "popcnt" || opName == "any_zero" {
		arg, err := parser.parseRegion()
		if err != nil {
			return sideRegion{}, err
		}

		if _, err := parser.expect(")"); err != nil {
			return sideRegion{}, err
		}

		cond := uint64(PredStorePopcnt)
		if opName == "any_zero" {
			cond = PredAnyZero
		}

		return sideRegion{
			predicate:     true,
			predicateOp:   cond,
			predicateSrc:  arg.reg,
			predicateSide: arg.side,
		}, nil
	}

	if opName == "argmin_nonzero" {
		value, key, err := parser.parseTwoBRegions()
		if err != nil {
			return sideRegion{}, err
		}

		return sideRegion{
			reduce:      true,
			reduceOp:    OpReduceArgMinNonZero,
			reduceValue: value.reg,
			reduceKey:   key.reg,
			reduceMatch: MaskTrue,
		}, nil
	}

	if opName == "mode_eq" {
		value, key, match, err := parser.parseModeEqRegions()
		if err != nil {
			return sideRegion{}, err
		}

		return sideRegion{
			reduce:      true,
			reduceOp:    OpReduceModeEq,
			reduceValue: value.reg,
			reduceKey:   key.reg,
			reduceMatch: match.reg,
		}, nil
	}

	if opName == "zipf_select" {
		value, key, temperature, err := parser.parseZipfSelectRegions()
		if err != nil {
			return sideRegion{}, err
		}

		return sideRegion{
			reduce:      true,
			reduceOp:    OpReduceZipfSelect,
			reduceValue: value.reg,
			reduceKey:   key.reg,
			reduceMatch: temperature.reg,
		}, nil
	}

	// Two-argument truth-table ops.
	lhs, err := parser.parseRegion()
	if err != nil {
		return sideRegion{}, err
	}

	if _, err := parser.expect(","); err != nil {
		return sideRegion{}, err
	}

	rhs, err := parser.parseRegion()
	if err != nil {
		return sideRegion{}, err
	}

	if _, err := parser.expect(")"); err != nil {
		return sideRegion{}, err
	}

	opcode, ok := opcodeOf(opName)
	if !ok {
		return sideRegion{}, fmt.Errorf("compiler: unknown op %q", opName)
	}

	// Cache the binary op shape by stuffing the operands into the AssignNode
	// directly; the caller will pull them out in makeAssignFromRHS.
	return sideRegion{
		side:          'A',
		reg:           Region{},
		predicate:     false,
		predicateOp:   opcode,
		predicateSrc:  lhs.reg,
		predicateSide: lhs.side,
		// repurpose unused storage for the second operand: keep it simple
		// by stashing rhs into reg + a marker side; the caller checks for
		// predicateOp != 0 to know it's a binary op.
	}, parser.stashBinop(opcode, lhs, rhs)
}

func (parser *parser) parseTwoBRegions() (sideRegion, sideRegion, error) {
	lhs, err := parser.parseRegion()
	if err != nil {
		return sideRegion{}, sideRegion{}, err
	}

	if lhs.side != 'B' {
		return sideRegion{}, sideRegion{}, fmt.Errorf("compiler: reducer value source must be B-side")
	}

	if _, err := parser.expect(","); err != nil {
		return sideRegion{}, sideRegion{}, err
	}

	rhs, err := parser.parseRegion()
	if err != nil {
		return sideRegion{}, sideRegion{}, err
	}

	if rhs.side != 'B' {
		return sideRegion{}, sideRegion{}, fmt.Errorf("compiler: reducer key source must be B-side")
	}

	if _, err := parser.expect(")"); err != nil {
		return sideRegion{}, sideRegion{}, err
	}

	return lhs, rhs, nil
}

func (parser *parser) parseModeEqRegions() (sideRegion, sideRegion, sideRegion, error) {
	value, key, err := parser.parseTwoBRegionsPrefix()
	if err != nil {
		return sideRegion{}, sideRegion{}, sideRegion{}, err
	}

	if _, err := parser.expect(","); err != nil {
		return sideRegion{}, sideRegion{}, sideRegion{}, err
	}

	match, err := parser.parseRegion()
	if err != nil {
		return sideRegion{}, sideRegion{}, sideRegion{}, err
	}

	if match.side != 'A' {
		return sideRegion{}, sideRegion{}, sideRegion{}, fmt.Errorf("compiler: mode_eq match source must be A-side")
	}

	if _, err := parser.expect(")"); err != nil {
		return sideRegion{}, sideRegion{}, sideRegion{}, err
	}

	return value, key, match, nil
}

func (parser *parser) parseZipfSelectRegions() (sideRegion, sideRegion, sideRegion, error) {
	value, key, err := parser.parseTwoBRegionsPrefix()
	if err != nil {
		return sideRegion{}, sideRegion{}, sideRegion{}, err
	}

	if _, err := parser.expect(","); err != nil {
		return sideRegion{}, sideRegion{}, sideRegion{}, err
	}

	temperature, err := parser.parseRegion()
	if err != nil {
		return sideRegion{}, sideRegion{}, sideRegion{}, err
	}

	if temperature.side != 'A' {
		return sideRegion{}, sideRegion{}, sideRegion{}, fmt.Errorf("compiler: zipf_select temperature source must be A-side")
	}

	if _, err := parser.expect(")"); err != nil {
		return sideRegion{}, sideRegion{}, sideRegion{}, err
	}

	return value, key, temperature, nil
}

func (parser *parser) parseTwoBRegionsPrefix() (sideRegion, sideRegion, error) {
	value, err := parser.parseRegion()
	if err != nil {
		return sideRegion{}, sideRegion{}, err
	}

	if value.side != 'B' {
		return sideRegion{}, sideRegion{}, fmt.Errorf("compiler: reducer value source must be B-side")
	}

	if _, err := parser.expect(","); err != nil {
		return sideRegion{}, sideRegion{}, err
	}

	key, err := parser.parseRegion()
	if err != nil {
		return sideRegion{}, sideRegion{}, err
	}

	if key.side != 'B' {
		return sideRegion{}, sideRegion{}, fmt.Errorf("compiler: reducer key source must be B-side")
	}

	return value, key, nil
}

// stashBinop is a small helper trick: the parser hands a single
// sideRegion back through parseRHS, but a binary truth-table op needs
// two operands. Rather than expand sideRegion further we recover the op
// shape via the parser's last-binop scratch state.
func (parser *parser) stashBinop(opcode uint64, lhs, rhs sideRegion) error {
	parser.lastBinop = &binop{opcode: opcode, lhs: lhs, rhs: rhs}
	return nil
}

type binop struct {
	opcode uint64
	lhs    sideRegion
	rhs    sideRegion
}

func opcodeOf(name string) (uint64, bool) {
	switch name {
	case "or":
		return OpOr, true
	case "and":
		return OpAnd, true
	case "xor":
		return OpXor, true
	case "and_not":
		return OpAandNotB, true
	case "truth_arrow":
		return OpIfBthenA, true
	case "nor":
		return OpNor, true
	case "xnor":
		return OpXnor, true
	case "nand":
		return OpNand, true
	case "not":
		return OpNotA, true
	}

	return 0, false
}

func geometricOpcodeOf(name string) (uint64, bool) {
	switch name {
	case "compose":
		return OpGeometricCompose, true
	case "sandwich":
		return OpGeometricSandwich, true
	case "reverse":
		return OpGeometricReverse, true
	}

	return 0, false
}

func isCallName(name string) bool {
	if name == "popcnt" || name == "any_zero" || name == "argmin_nonzero" || name == "mode_eq" || name == "zipf_select" {
		return true
	}

	_, ok := opcodeOf(name)
	return ok
}

func (parser *parser) makeAssignFromRHS(dst, rhs sideRegion) (ASTNode, error) {
	if parser.lastBinop != nil {
		op := parser.lastBinop
		parser.lastBinop = nil

		return parser.makeBinAssign(dst, op.opcode, op.lhs, op.rhs)
	}

	return parser.makeCopyAssign(dst, rhs)
}

/*
makeBinAssign places the A-side operand on the SrcA port and the
B-side operand on the SrcB port. When both operands are on B the kernel
needs to read SrcA from the popped frame too — the SrcAFromB bit makes
that explicit so `write B.tokens <- xor(B.tokens, B.signals[2,1])`
expresses what it reads.
*/
func (parser *parser) makeBinAssign(dst sideRegion, opcode uint64, a, b sideRegion) (ASTNode, error) {
	if a.side == 'C' || b.side == 'C' {
		return nil, fmt.Errorf("compiler: emitted child sources are not encodable")
	}

	srcA, srcB := a, b

	if a.side == 'B' && b.side == 'A' {
		srcA, srcB = b, a
	}

	if srcA.rotate != 0 {
		return nil, fmt.Errorf("compiler: rot8 can only be applied to the SrcB operand")
	}

	srcAFromB := uint64(0)
	if srcA.side == 'B' {
		srcAFromB = 1
	}

	return AssignNode{
		Dst:       dst.reg,
		SrcA:      srcA.reg,
		SrcB:      srcB.reg,
		Opcode:    opcode,
		Target:    targetOf(dst.side),
		SrcAFromB: srcAFromB,
		BRotate:   srcB.rotate,
	}, nil
}

func (parser *parser) makeCopyAssign(dst, src sideRegion) (ASTNode, error) {
	if src.side == 'C' {
		return nil, fmt.Errorf("compiler: emitted child sources are not encodable")
	}

	// `set X <- 0` clears X via OpFalse so multi-word destinations stay
	// fully zeroed without allocating a span-sized constant block.
	if src.literalZero {
		return AssignNode{
			Dst:    dst.reg,
			SrcA:   dst.reg,
			SrcB:   dst.reg,
			Opcode: OpFalse,
			Target: targetOf(dst.side),
		}, nil
	}

	// B-side sources go through OpCopyB so the kernel reads them from
	// the popped frame without needing a routing-bit toggle.
	opcode := uint64(OpCopyA)
	if src.side == 'B' {
		opcode = OpCopyB
	}

	return AssignNode{
		Dst:     dst.reg,
		SrcA:    src.reg,
		SrcB:    src.reg,
		Opcode:  opcode,
		Target:  targetOf(dst.side),
		BRotate: src.rotate,
	}, nil
}

func targetOf(side byte) uint64 {
	if side == 'B' {
		return 1
	}

	if side == 'C' {
		return 2
	}

	return 0
}

func (parser *parser) parsePop() (ASTNode, error) {
	parser.advance() // consume "pop"

	if _, err := parser.expect("("); err != nil {
		return nil, err
	}

	if _, err := parser.expect("ident"); err != nil {
		return nil, err
	}

	if _, err := parser.expect(")"); err != nil {
		return nil, err
	}

	if _, err := parser.expect("{"); err != nil {
		return nil, err
	}

	body, err := parser.parseStatements("}")
	if err != nil {
		return nil, err
	}

	if _, err := parser.expect("}"); err != nil {
		return nil, err
	}

	return PopBNode{Body: body}, nil
}

func (parser *parser) parseStage() (ASTNode, error) {
	parser.advance() // consume "stage"

	if _, err := parser.expect("("); err != nil {
		return nil, err
	}

	if _, err := parser.expect("ident"); err != nil {
		return nil, err
	}

	if _, err := parser.expect(")"); err != nil {
		return nil, err
	}

	return StageNode{}, nil
}

func (parser *parser) parseGossip() (ASTNode, error) {
	parser.advance() // consume "gossip"

	if _, err := parser.expect("("); err != nil {
		return nil, err
	}

	if _, err := parser.expect("ident"); err != nil {
		return nil, err
	}

	if _, err := parser.expect(")"); err != nil {
		return nil, err
	}

	if _, err := parser.expect("{"); err != nil {
		return nil, err
	}

	body, err := parser.parseStatements("}")
	if err != nil {
		return nil, err
	}

	if _, err := parser.expect("}"); err != nil {
		return nil, err
	}

	return GossipNode{Body: body}, nil
}

func (parser *parser) parseEmit() (ASTNode, error) {
	parser.advance() // consume "emit"

	if _, err := parser.expect("{"); err != nil {
		return nil, err
	}

	parser.emitDepth++
	defer func() {
		parser.emitDepth--
	}()

	body, err := parser.parseStatements("}")
	if err != nil {
		return nil, err
	}

	if _, err := parser.expect("}"); err != nil {
		return nil, err
	}

	return EmitNode{Body: body}, nil
}

/*
parseRegion accepts:

  - numeric literal:                      120
  - status / role / property enums:       DONE, READOUT
  - bare A-side regions:                  program, child
  - dotted region paths:                  A.affinity, B.signals
  - property paths:                       A.properties.status
  - sliced regions:                       A.signals[0,8]
*/
func (parser *parser) parseRegion() (sideRegion, error) {
	if parser.peek().kind == "{{" {
		return parser.parseIndirectRegion('A')
	}

	tok := parser.advance()

	if tok.kind == "num" {
		val, err := strconv.ParseUint(tok.text, 10, 64)
		if err != nil {
			return sideRegion{}, err
		}

		if val == 0 {
			return sideRegion{side: 'A', reg: parser.builder.AllocConstant(0), literalZero: true}, nil
		}

		return sideRegion{side: 'A', reg: parser.builder.AllocConstant(val)}, nil
	}

	if tok.kind != "ident" {
		return sideRegion{}, fmt.Errorf("compiler: expected region, got %q (%q)", tok.kind, tok.text)
	}

	if value, ok := StatusEnum[tok.text]; ok {
		return parser.constRegion(value), nil
	}

	if value, ok := RoleEnum[tok.text]; ok {
		return parser.constRegion(value), nil
	}

	if tok.text == "rot8" {
		return parser.parseRot8Region()
	}

	if tok.text == "program" {
		return parser.maybeIndex(sideRegion{side: parser.defaultRegionSide(), reg: Program})
	}

	if tok.text == "asset" {
		return parser.maybeIndex(sideRegion{side: parser.defaultRegionSide(), reg: Asset})
	}

	if tok.text == "context" {
		return parser.maybeIndex(sideRegion{side: parser.defaultRegionSide(), reg: Context})
	}

	if tok.text == "signals" {
		return parser.maybeIndex(sideRegion{side: parser.defaultRegionSide(), reg: Signals})
	}

	if tok.text == "gradient" {
		return parser.maybeIndex(sideRegion{side: parser.defaultRegionSide(), reg: Gradient})
	}

	if tok.text == "affinity" {
		return parser.maybeIndex(sideRegion{side: parser.defaultRegionSide(), reg: Affinity})
	}

	if tok.text == "properties" && parser.emitDepth > 0 {
		return parser.parsePropertyPath(parser.defaultRegionSide())
	}

	if tok.text == "A" || tok.text == "B" {
		side := tok.text[0]

		if _, err := parser.expect("."); err != nil {
			return sideRegion{}, err
		}

		return parser.parseRegionPath(side)
	}

	return sideRegion{}, fmt.Errorf("compiler: unknown region root %q", tok.text)
}

func (parser *parser) defaultRegionSide() byte {
	if parser.emitDepth > 0 {
		return 'C'
	}

	return 'A'
}

/*
parseIndirectRegion handles the `{{ inner }}` form. The inner expression
must resolve to a single-word region; its .Start is the INDIRECTION
ADDRESS — the frame word the host will read at install time to fill in
the actual operand. Two surface forms compose into this:

  - `B.{{A.context[0,1]}}` — B-side operand whose start is read from
    A.context[0] when the firmware is installed on a Value.
  - `{{A.context[1,1]}}`   — scalar threshold whose value is read from
    A.context[1] at install and copied into the asset slot the kernel
    uses as the comparison operand.

The compiler does not interpret the inner reference at compile time. It
records a substitution slot keyed off the inner region's start address;
InstallFirmware reads the live word from the Value frame and patches
the packed instruction (operand) or the asset slot (scalar) accordingly.
*/
func (parser *parser) parseIndirectRegion(side byte) (sideRegion, error) {
	if _, err := parser.expect("{{"); err != nil {
		return sideRegion{}, err
	}

	inner, err := parser.parseRegion()
	if err != nil {
		return sideRegion{}, err
	}

	if inner.indirect {
		return sideRegion{}, fmt.Errorf("compiler: nested {{...}} is not supported")
	}

	if _, err := parser.expect("}}"); err != nil {
		return sideRegion{}, err
	}

	if side == 'A' {
		slot := parser.builder.AllocConstant(0)
		parser.builder.RecordConstantSubstitution(slot.Start, inner.reg.Start)

		return sideRegion{
			side: side,
			reg:  slot,
		}, nil
	}

	return sideRegion{
		side:     side,
		reg:      Region{Start: inner.reg.Start, Span: 1},
		indirect: true,
	}, nil
}

func (parser *parser) parseRot8Region() (sideRegion, error) {
	if _, err := parser.expect("("); err != nil {
		return sideRegion{}, err
	}

	region, err := parser.parseRegion()
	if err != nil {
		return sideRegion{}, err
	}

	if region.side != 'B' {
		return sideRegion{}, fmt.Errorf("compiler: rot8 only accepts B-side regions")
	}

	if region.rotate != 0 {
		return sideRegion{}, fmt.Errorf("compiler: nested rot8 is not supported")
	}

	if _, err := parser.expect(","); err != nil {
		return sideRegion{}, err
	}

	stepsTok, err := parser.expect("num")
	if err != nil {
		return sideRegion{}, err
	}

	if _, err := parser.expect(")"); err != nil {
		return sideRegion{}, err
	}

	steps, err := strconv.ParseUint(stepsTok.text, 10, 64)
	if err != nil {
		return sideRegion{}, err
	}

	if steps > 7 {
		return sideRegion{}, fmt.Errorf("compiler: rot8 step must be in [0,7]")
	}

	region.rotate = steps

	return region, nil
}

func (parser *parser) constRegion(value uint64) sideRegion {
	region := parser.builder.AllocConstant(value)

	side := sideRegion{side: 'A', reg: region}
	if value == 0 {
		side.literalZero = true
	}

	return side
}

/*
parseRegionPath consumes the right-hand side of `A.` / `B.` / `child.`
and returns the resolved region with its operand side. Slices are
applied via maybeIndex, which trims an existing region down to the
[start, span] window the source requested.
*/
func (parser *parser) parseRegionPath(side byte) (sideRegion, error) {
	if parser.peek().kind == "{{" {
		return parser.parseIndirectRegion(side)
	}

	tok, err := parser.expect("ident")
	if err != nil {
		return sideRegion{}, err
	}

	switch tok.text {
	case "tokens":
		return parser.maybeIndex(sideRegion{side: side, reg: Tokens})
	case "signals":
		return parser.maybeIndex(sideRegion{side: side, reg: Signals})
	case "context":
		return parser.maybeIndex(sideRegion{side: side, reg: Context})
	case "gradient":
		return parser.maybeIndex(sideRegion{side: side, reg: Gradient})
	case "asset":
		return parser.maybeIndex(sideRegion{side: side, reg: Asset})
	case "affinity":
		return parser.maybeIndex(sideRegion{side: side, reg: Affinity})
	case "program":
		return parser.maybeIndex(sideRegion{side: side, reg: Program})
	case "id":
		return sideRegion{side: side, reg: ID}, nil
	case "prev":
		return sideRegion{side: side, reg: Prev}, nil
	case "next":
		return sideRegion{side: side, reg: Next}, nil
	case "properties":
		return parser.parsePropertyPath(side)
	}

	return sideRegion{}, fmt.Errorf("compiler: unknown region path %q", tok.text)
}

func (parser *parser) parsePropertyPath(side byte) (sideRegion, error) {
	if _, err := parser.expect("."); err != nil {
		return sideRegion{}, err
	}

	prop, err := parser.expect("ident")
	if err != nil {
		return sideRegion{}, err
	}

	offset, ok := PropertyOffsets[prop.text]
	if !ok {
		return sideRegion{}, fmt.Errorf("compiler: unknown property %q", prop.text)
	}

	return sideRegion{side: side, reg: Region{Start: offset, Span: 1}}, nil
}

/*
maybeIndex consumes an optional `[start, span]` suffix and narrows the
region to that window in word-coordinates relative to its own start.
*/
func (parser *parser) maybeIndex(base sideRegion) (sideRegion, error) {
	if parser.peek().kind != "[" {
		return base, nil
	}

	parser.advance() // [

	startTok, err := parser.expect("num")
	if err != nil {
		return sideRegion{}, err
	}

	if _, err := parser.expect(","); err != nil {
		return sideRegion{}, err
	}

	spanTok, err := parser.expect("num")
	if err != nil {
		return sideRegion{}, err
	}

	if _, err := parser.expect("]"); err != nil {
		return sideRegion{}, err
	}

	start, err := strconv.ParseUint(startTok.text, 10, 64)
	if err != nil {
		return sideRegion{}, err
	}

	span, err := strconv.ParseUint(spanTok.text, 10, 64)
	if err != nil {
		return sideRegion{}, err
	}

	if span == 0 {
		span = 1
	}

	return sideRegion{
		side:   base.side,
		reg:    Region{Start: base.reg.Start + start, Span: span},
		rotate: base.rotate,
	}, nil
}
