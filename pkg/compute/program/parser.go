package program

import (
	"errors"
	"fmt"
	"strings"
)

type Parser struct {
	scanner *Scanner
	tokens  []Token
	pos     int
}

func NewParser(input string) *Parser {
	p := &Parser{
		scanner: NewScanner(input),
	}
	for tok := range p.scanner.Scan() {
		p.tokens = append(p.tokens, tok)
	}
	return p
}

func (p *Parser) next() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	tok := p.tokens[p.pos]
	p.pos++
	return tok
}

func (p *Parser) peek() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *Parser) backup() {
	if p.pos > 0 {
		p.pos--
	}
}

func (p *Parser) expect(t TokenType) (Token, error) {
	tok := p.next()
	if tok.Type != t {
		return tok, fmt.Errorf("expected %v, got %v (%q)", t, tok.Type, tok.Value)
	}
	return tok, nil
}

// AST Nodes
type Node interface {
	node()
}

type InstructionNode struct {
	Target    *TargetNode
	Expr      *ExprNode
	Predicate *PredicateNode
	Scope     string
}

func (n *InstructionNode) node() {}

type TargetNode struct {
	Region   string
	Topology string
}

type ExprNode struct {
	Mode string // "truth", "popcnt", "any_zero", "all_ones"
	A    string
	Op   string
	B    string
}

type PredicateNode struct {
	Region    string
	Op        string
	Value     string
	IsPopcnt  bool
	Threshold string
}

func (p *Parser) ParseInstruction() (*InstructionNode, error) {
	// Parse [ (Target) <= (Expr) ? (Predicate) <= Scope ]
	_, err := p.expect(TokenLBracket)
	if err != nil {
		return nil, err
	}

	target, err := p.parseTarget()
	if err != nil {
		return nil, err
	}

	_, err = p.expect(TokenFeed)
	if err != nil {
		return nil, err
	}

	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}

	var pred *PredicateNode
	tok := p.peek()
	if tok.Type == TokenGate {
		p.next() // consume ?
		pred, err = p.parsePredicate()
		if err != nil {
			return nil, err
		}
	}

	// We might have an optional Scope
	scope := "community"
	tok = p.peek()
	if tok.Type == TokenFeed {
		p.next() // consume <=

		// Scope could be `community` or `(0..n)`
		scopeTok := p.next()
		if scopeTok.Type == TokenLParen {
			scope = "("
			for {
				inner := p.next()
				scope += inner.Value
				if inner.Type == TokenRParen {
					break
				}
			}
		} else {
			scope = scopeTok.Value
		}
	}

	_, err = p.expect(TokenRBracket)
	if err != nil {
		return nil, err
	}

	return &InstructionNode{
		Target:    target,
		Expr:      expr,
		Predicate: pred,
		Scope:     scope,
	}, nil
}

func (p *Parser) parseTarget() (*TargetNode, error) {
	_, err := p.expect(TokenLParen)
	if err != nil {
		return nil, err
	}

	// Read everything until RParen
	var parts []string
	for {
		tok := p.next()
		if tok.Type == TokenRParen {
			break
		}
		if tok.Type == TokenEOF {
			return nil, errors.New("unexpected EOF in target")
		}
		parts = append(parts, tok.Value)
	}

	if len(parts) != 2 {
		return nil, fmt.Errorf("target must be 'Region Topology', got %v", parts)
	}

	return &TargetNode{
		Region:   parts[0],
		Topology: parts[1],
	}, nil
}

func (p *Parser) parseExpr() (*ExprNode, error) {
	mode := "truth"

	// Check for reduction functions: popcnt(A), any_zero(A), all_ones(A)
	tok := p.peek()
	if tok.Type == TokenIdent && (tok.Value == "popcnt" || tok.Value == "any_zero" || tok.Value == "all_ones" || tok.Value == "saturates") {
		mode = tok.Value
		p.next() // consume func name
	}

	// Parenthesized (a ^ b) and bare `DONE` / `A` are both valid; the bare
	// case must be recognized before expect(TokenLParen), because expect
	// would consume the identifier and then the legacy fallback would
	// incorrectly consume the closing `]`.
	tok = p.peek()
	if tok.Type == TokenIdent && (tok.Value == "DONE" || tok.Value == "A") {
		p.next()

		return &ExprNode{Mode: mode, A: tok.Value}, nil
	}

	_, err := p.expect(TokenLParen)
	if err != nil {
		return nil, err
	}

	var parts []string
	for {
		tok := p.next()
		if tok.Type == TokenRParen {
			break
		}
		if tok.Type == TokenEOF {
			return nil, errors.New("unexpected EOF in expr")
		}
		// Combine multi-character operators that were split, like `-` and `>`
		if tok.Type == TokenOp && tok.Value == "-" && p.peek().Type == TokenRAngle {
			tok.Value += p.next().Value
		} else if tok.Type == TokenLAngle && p.peek().Type == TokenOp && p.peek().Value == "-" {
			tok.Value += p.next().Value
		}
		parts = append(parts, tok.Value)
	}

	if len(parts) == 0 {
		return &ExprNode{Mode: mode}, nil
	} else if len(parts) == 1 {
		return &ExprNode{Mode: mode, A: parts[0]}, nil
	} else if len(parts) == 2 && strings.EqualFold(parts[1], "A") {
		return &ExprNode{Mode: mode, A: parts[0], Op: "A"}, nil
	} else if len(parts) == 3 {
		return &ExprNode{Mode: mode, A: parts[0], Op: parts[1], B: parts[2]}, nil
	}

	return nil, fmt.Errorf("invalid expr: %v", parts)
}

func (p *Parser) parsePredicate() (*PredicateNode, error) {
	_, err := p.expect(TokenLParen)
	if err != nil {
		return nil, err
	}

	var parts []string
	for {
		tok := p.next()
		if tok.Type == TokenRParen {
			break
		}
		if tok.Type == TokenEOF {
			return nil, errors.New("unexpected EOF in predicate")
		}
		// Handle nested parens for popcnt(A)
		if tok.Type == TokenLParen {
			parts[len(parts)-1] += "("
			for {
				inner := p.next()
				parts[len(parts)-1] += inner.Value
				if inner.Type == TokenRParen {
					break
				}
			}
			continue
		}
		// Combine multi-character operators like `!` and `=`
		if tok.Type == TokenNot && p.peek().Value == "=" {
			tok.Value += p.next().Value
		} else if tok.Value == "=" && p.peek().Value == "=" {
			tok.Value += p.next().Value
		}
		parts = append(parts, tok.Value)
	}

	if len(parts) != 3 {
		return nil, fmt.Errorf("predicate must be 'Region OP Value', got %v", parts)
	}

	if strings.HasPrefix(parts[0], "popcnt(") {
		return &PredicateNode{
			IsPopcnt:  true,
			Region:    parts[0][7 : len(parts[0])-1],
			Op:        parts[1],
			Threshold: parts[2],
		}, nil
	}

	return &PredicateNode{
		Region: parts[0],
		Op:     parts[1],
		Value:  parts[2],
	}, nil
}
