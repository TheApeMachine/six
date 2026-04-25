package program

import (
	"iter"
	"unicode"
	"unicode/utf8"
)

type TokenType int

const (
	TokenEOF TokenType = iota
	TokenError
	TokenLBracket // [
	TokenRBracket // ]
	TokenLBrace   // {
	TokenRBrace   // }
	TokenLAngle   // <
	TokenRAngle   // >
	TokenFeed     // <=
	TokenGate     // ?
	TokenNot      // !
	TokenIdent    // A, B, popcnt, etc.
	TokenNumber   // 120
	TokenOp       // ^, |, ==, etc.
	TokenLParen   // (
	TokenRParen   // )
)

type Token struct {
	Type  TokenType
	Value string
}

type Scanner struct {
	input string
	start int
	pos   int
	width int
}

func NewScanner(input string) *Scanner {
	return &Scanner{input: input}
}

func (s *Scanner) next() rune {
	if s.pos >= len(s.input) {
		s.width = 0
		return 0
	}
	r, w := utf8.DecodeRuneInString(s.input[s.pos:])
	s.width = w
	s.pos += s.width
	return r
}

func (s *Scanner) backup() {
	s.pos -= s.width
}

func (s *Scanner) peek() rune {
	r := s.next()
	s.backup()
	return r
}

func (s *Scanner) skipWhitespaceAndComments() {
	for {
		r := s.next()
		if r == 0 {
			break
		}
		if unicode.IsSpace(r) {
			continue
		}
		if r == ';' || r == '#' {
			// Skip to end of line
			for {
				r := s.next()
				if r == '\n' || r == 0 {
					break
				}
			}
			continue
		}
		s.backup()
		break
	}
}

func (s *Scanner) Scan() iter.Seq[Token] {
	return func(yield func(Token) bool) {
		for {
			s.skipWhitespaceAndComments()
			if s.pos >= len(s.input) {
				break
			}

			s.start = s.pos
			r := s.next()

			var tok TokenType
			switch r {
			case '[':
				tok = TokenLBracket
			case ']':
				tok = TokenRBracket
			case '{':
				tok = TokenLBrace
			case '}':
				tok = TokenRBrace
			case '(':
				tok = TokenLParen
			case ')':
				tok = TokenRParen
			case '?':
				tok = TokenGate
			case '!':
				tok = TokenNot
			case '<':
				if s.peek() == '=' {
					s.next()
					tok = TokenFeed
				} else {
					tok = TokenLAngle
				}
			case '>':
				tok = TokenRAngle
			case '^', '|', '&', '~', '=', '\\', '/', '-':
				// Handle multi-char ops like ==, ~|, ->, <-
				if p := s.peek(); p == '=' || p == '|' || p == '&' || p == 'B' || p == 'A' || p == '>' {
					s.next()
				}
				tok = TokenOp
			default:
				if unicode.IsLetter(r) {
					for {
						p := s.peek()
						if unicode.IsLetter(p) || unicode.IsDigit(p) || p == '_' || p == '.' || p == '[' || p == ']' || p == ',' {
							// Added [, ], and , to Ident characters so things like `asset[0,1]` and `properties.status` are parsed as a single TokenIdent.
							s.next()
						} else {
							break
						}
					}
					tok = TokenIdent
				} else if unicode.IsDigit(r) {
					for {
						p := s.peek()
						if unicode.IsDigit(p) || p == '.' {
							// Added . to handle ranges like 16..24
							s.next()
						} else {
							break
						}
					}
					tok = TokenNumber
				} else {
					tok = TokenError
				}
			}

			if !yield(Token{Type: tok, Value: s.input[s.start:s.pos]}) {
				return
			}
		}
	}
}
