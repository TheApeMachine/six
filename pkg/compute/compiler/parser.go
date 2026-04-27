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
		Words:        parser.builder.Compile(),
		Constants:    parser.builder.Constants(),
		MaskTrueWord: MaskTrue.Start,
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
	side byte
	reg  Region
	// predicate set when the RHS is a popcnt/any-zero reduction; if so
	// the destination receives a folded scalar from the popcount engine
	// instead of a bitwise truth-table op.
	predicate     bool
	predicateOp   uint64
	predicateSrc  Region
	predicateSide byte
	// literalZero distinguishes `set X <- 0` (which lowers to OpFalse
	// across the full destination span) from a generic single-word
	// constant allocation that happens to hold zero.
	literalZero bool
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
		}
	}

	return nil, fmt.Errorf("compiler: unexpected token %q (%q) at pos %d", tok.kind, tok.text, parser.pos)
}

func (parser *parser) parseIf() (ASTNode, error) {
	if _, err := parser.expect("("); err != nil {
		return nil, err
	}

	cond, region, threshold, err := parser.parseComparison()
	if err != nil {
		return nil, err
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
		Cond:      cond,
		Region:    region,
		Threshold: threshold,
		Mask:      mask,
		Body:      body,
	}, nil
}

func (parser *parser) parseComparison() (cond uint64, region, threshold Region, err error) {
	lhs, err := parser.parseRegion()
	if err != nil {
		return 0, Region{}, Region{}, err
	}

	cmpTok := parser.advance()

	cond, ok := condOf(cmpTok.text)
	if !ok {
		return 0, Region{}, Region{}, fmt.Errorf("compiler: unknown comparison %q", cmpTok.text)
	}

	rhs, err := parser.parseRegion()
	if err != nil {
		return 0, Region{}, Region{}, err
	}

	return cond, lhs.reg, rhs.reg, nil
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
			Dst:    dst.reg,
			Cond:   rhs.predicateOp,
			Region: rhs.predicateSrc,
			Target: targetOf(dst.side),
		}, nil
	}

	return parser.makeAssignFromRHS(dst, rhs)
}

func (parser *parser) parseRHS() (sideRegion, error) {
	tok := parser.peek()

	if tok.kind == "ident" && parser.lookahead(1).kind == "(" {
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
	srcA, srcB := a, b

	if a.side == 'B' && b.side == 'A' {
		srcA, srcB = b, a
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
	}, nil
}

func (parser *parser) makeCopyAssign(dst, src sideRegion) (ASTNode, error) {
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
		Dst:    dst.reg,
		SrcA:   src.reg,
		SrcB:   src.reg,
		Opcode: opcode,
		Target: targetOf(dst.side),
	}, nil
}

func targetOf(side byte) uint64 {
	if side == 'B' {
		return 1
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

	if tok.text == "program" {
		return parser.maybeIndex(sideRegion{side: 'A', reg: Program})
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

	return sideRegion{}, fmt.Errorf("compiler: unknown region path %q", tok.text)
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
		side: base.side,
		reg:  Region{Start: base.reg.Start + start, Span: span},
	}, nil
}

