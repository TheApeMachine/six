package ts

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
)

const NestedTokenSep = "\x1f"

const (
	TermCall      = "call"
	TermOwner     = "owner"
	TermRef       = "ref"
	TermNumber    = "number"
	TermOperator  = "operator"
	TermReducer   = "reducer"
	TermTopology  = "topology"
	TermQuestion  = "question"
	TermOperation = "operation"
)

/*
PipeSite is one bracket pipe in document order.
Emit is true for pipes that live under an emit_block (<[ ... ]>).
*/
type PipeSite struct {
	Emit      bool
	StartByte uint
	EndByte   uint
}

/*
Term is a Tree-sitter term with the CST details needed by the program lowerer.
Calls carry owner/ref/rotation directly so callers never need to rediscover the
inside of A(...) or B(...).
*/
type Term struct {
	Kind   string
	Text   string
	Owner  string
	Ref    string
	Rotate int
	Terms  []Term
}

/*
Operation is one top-level `{ ... }` block with Tree-sitter-tokenized feed
terms. Nested operations are represented as a single token containing their
own terms, which keeps rotated operands and predicate operands atomic for the
lowerer.
*/
type Operation struct {
	Tokens  []string
	Terms   []Term
	Surface string
}

/*
PipeSites parses source once and returns pipe spans in order.
*/
func PipeSites(ctx context.Context, source []byte) ([]PipeSite, error) {
	tree, err := Parse(ctx, source)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	root := tree.RootNode()
	if root.Type() != "source_file" {
		return nil, fmt.Errorf("ts: unexpected root %q", root.Type())
	}

	if root.HasError() {
		return nil, syntaxError(source, root)
	}

	var sites []PipeSite

	for idx := uint32(0); idx < root.NamedChildCount(); idx++ {
		ch := root.NamedChild(idx)

		switch ch.Type() {
		case "feed":
			continue

		case "pipe":
			sites = append(sites, pipeSite(false, ch))

		case "emit_block":
			var emitPipes int

			for cidx := uint32(0); cidx < ch.ChildCount(); cidx++ {
				node := ch.Child(cidx)
				if node.Type() != "pipe" {
					continue
				}

				sites = append(sites, pipeSite(true, node))
				emitPipes++
			}

			if emitPipes == 0 {
				return nil, fmt.Errorf("ts: emit_block without pipe")
			}

		default:
			return nil, fmt.Errorf("ts: unexpected node %q under source_file", ch.Type())
		}
	}

	if len(sites) == 0 {
		return nil, fmt.Errorf("feed source contains no pipes")
	}

	return sites, nil
}

/*
FeedSite is a pipe inner source slice for callers that only need text (tests, tooling).
*/
type FeedSite struct {
	Emit         bool
	Body         string
	Compact      []string
	CompactTerms []Term
	Operations   []Operation
}

/*
FeedProgram is the Tree-sitter shape the compiler consumes.
HasFeed is true when the source contains an explicit <= bond.
*/
type FeedProgram struct {
	Sites   []FeedSite
	HasFeed bool
}

/*
FeedSites is PipeSites plus extraction of the [ ... ] interior as syntax terms.
*/
func FeedSites(ctx context.Context, source []byte) ([]FeedSite, error) {
	program, err := ParseFeedProgram(ctx, source)
	if err != nil {
		return nil, err
	}

	return program.Sites, nil
}

/*
ParseFeedProgram parses source into feed sites and top-level feed metadata.
*/
func ParseFeedProgram(ctx context.Context, source []byte) (FeedProgram, error) {
	tree, err := Parse(ctx, source)
	if err != nil {
		return FeedProgram{}, err
	}
	defer tree.Close()

	root := tree.RootNode()
	if root.Type() != "source_file" {
		return FeedProgram{}, fmt.Errorf("ts: unexpected root %q", root.Type())
	}

	if root.HasError() {
		return FeedProgram{}, syntaxError(source, root)
	}

	var program FeedProgram

	for idx := uint32(0); idx < root.NamedChildCount(); idx++ {
		ch := root.NamedChild(idx)

		switch ch.Type() {
		case "feed":
			program.HasFeed = true
			continue

		case "pipe":
			site, err := feedSiteFromPipe(false, ch, source)
			if err != nil {
				return FeedProgram{}, err
			}

			program.Sites = append(program.Sites, site)

		case "emit_block":
			emitSites, hasFeed, err := feedSitesFromEmitBlock(ch, source)
			if err != nil {
				return FeedProgram{}, err
			}
			if hasFeed {
				program.HasFeed = true
			}
			program.Sites = append(program.Sites, emitSites...)

		default:
			return FeedProgram{}, fmt.Errorf("ts: unexpected node %q under source_file", ch.Type())
		}
	}

	if len(program.Sites) == 0 {
		return FeedProgram{}, fmt.Errorf("feed source contains no pipes")
	}

	return program, nil
}

func feedSitesFromEmitBlock(node sitter.Node, source []byte) ([]FeedSite, bool, error) {
	var sites []FeedSite
	var hasFeed bool

	for cidx := uint32(0); cidx < node.ChildCount(); cidx++ {
		child := node.Child(cidx)

		switch child.Type() {
		case "feed":
			hasFeed = true

		case "pipe":
			site, err := feedSiteFromPipe(true, child, source)
			if err != nil {
				return nil, false, err
			}

			sites = append(sites, site)
		}
	}

	if len(sites) == 0 {
		return nil, false, fmt.Errorf("ts: emit_block without pipe")
	}

	return sites, hasFeed, nil
}

/*
PipeInnerBytes returns the trimmed interior of a pipe span (between [ and ]).
*/
func PipeInnerBytes(pipe PipeSite, source []byte) (string, error) {
	start := pipe.StartByte
	end := pipe.EndByte
	if end < start+2 {
		return "", fmt.Errorf("ts: invalid pipe span")
	}

	if source[start] != '[' || source[end-1] != ']' {
		return "", fmt.Errorf("ts: pipe node does not span [ ]")
	}

	return strings.TrimSpace(string(source[start+1 : end-1])), nil
}

func pipeSite(emit bool, node sitter.Node) PipeSite {
	return PipeSite{
		Emit:      emit,
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
	}
}

func feedSiteFromPipe(emit bool, node sitter.Node, source []byte) (FeedSite, error) {
	span := pipeSite(emit, node)
	body, err := PipeInnerBytes(span, source)
	if err != nil {
		return FeedSite{}, err
	}

	site := FeedSite{Emit: emit, Body: body}

	for idx := uint32(0); idx < node.ChildCount(); idx++ {
		child := node.Child(idx)
		if child.IsNull() {
			continue
		}

		switch child.Type() {
		case "operation":
			site.Operations = append(site.Operations, operationFromNode(child, source))
		case "pipe_body_compact":
			site.CompactTerms = termsFromNode(child, source)
			site.Compact = tokensFromTerms(site.CompactTerms)
		case "[", "]":
			continue
		default:
			if child.IsNamed() {
				terms := termsFromNode(child, source)
				site.CompactTerms = append(site.CompactTerms, terms...)
				site.Compact = append(site.Compact, tokensFromTerms(terms)...)
			}
		}
	}

	return site, nil
}

func operationFromNode(node sitter.Node, source []byte) Operation {
	terms := termsFromNode(node, source)

	return Operation{
		Tokens:  tokensFromTerms(terms),
		Terms:   terms,
		Surface: strings.TrimSpace(string(source[node.StartByte():node.EndByte()])),
	}
}

func termsFromNode(node sitter.Node, source []byte) []Term {
	var terms []Term

	for idx := uint32(0); idx < node.ChildCount(); idx++ {
		child := node.Child(idx)
		if child.IsNull() {
			continue
		}

		switch child.Type() {
		case "{", "}", "[", "]", "(", ")":
			continue
		case "call":
			terms = append(terms, callTermFromNode(child, source))
		case "owner":
			terms = append(terms, textTerm(TermOwner, child, source))
		case "ref":
			terms = append(terms, textTerm(TermRef, child, source))
		case "number":
			terms = append(terms, textTerm(TermNumber, child, source))
		case "operator":
			terms = append(terms, textTerm(TermOperator, child, source))
		case "reducer":
			terms = append(terms, textTerm(TermReducer, child, source))
		case "topology":
			terms = append(terms, textTerm(TermTopology, child, source))
		case "question":
			terms = append(terms, textTerm(TermQuestion, child, source))
		case "operation":
			nested := operationFromNode(child, source)
			if len(nested.Terms) > 0 {
				terms = append(terms, Term{
					Kind:  TermOperation,
					Text:  nested.Surface,
					Terms: nested.Terms,
				})
			}
		default:
			if child.IsNamed() {
				terms = append(terms, termsFromNode(child, source)...)
			}
		}
	}

	return terms
}

func callTermFromNode(node sitter.Node, source []byte) Term {
	term := Term{
		Kind: TermCall,
		Text: tokenFromNode(node, source),
	}

	for idx := uint32(0); idx < node.NamedChildCount(); idx++ {
		child := node.NamedChild(idx)

		switch child.Type() {
		case "owner":
			term.Owner = tokenFromNode(child, source)
		case "ref":
			term.Ref = tokenFromNode(child, source)
		case "rotation":
			term.Rotate = rotationFromNode(child, source)
		}
	}

	return term
}

func rotationFromNode(node sitter.Node, source []byte) int {
	var amount int
	var shift string

	for idx := uint32(0); idx < node.NamedChildCount(); idx++ {
		child := node.NamedChild(idx)

		switch child.Type() {
		case "number":
			amount, _ = strconv.Atoi(tokenFromNode(child, source))
		case "shift":
			shift = tokenFromNode(child, source)
		}
	}

	if shift == ">>" {
		return -amount
	}

	return amount
}

func textTerm(kind string, node sitter.Node, source []byte) Term {
	return Term{
		Kind: kind,
		Text: tokenFromNode(node, source),
	}
}

func tokensFromTerms(terms []Term) []string {
	var tokens []string

	for _, term := range terms {
		switch term.Kind {
		case TermCall:
			text := term.Text
			if text == "" {
				text = term.Owner + "(" + term.Ref + ")"
			}
			tokens = append(tokens, text)
		case TermOperation:
			nested := tokensFromTerms(term.Terms)
			if len(nested) > 0 {
				tokens = append(tokens, strings.Join(nested, NestedTokenSep))
			}
		default:
			tokens = append(tokens, term.Text)
		}
	}

	return tokens
}

func tokenFromNode(node sitter.Node, source []byte) string {
	return strings.TrimSpace(string(source[node.StartByte():node.EndByte()]))
}

func syntaxError(source []byte, root sitter.Node) error {
	node := firstError(root)
	if node.IsNull() {
		return fmt.Errorf("ts: syntax error in feed source")
	}

	point := node.StartPoint()
	near := sourceLine(source, node.StartByte())

	return fmt.Errorf("ts: syntax error in feed source at %d:%d near %q", point.Row+1, point.Column+1, near)
}

func firstError(node sitter.Node) sitter.Node {
	if node.IsNull() || node.IsError() || node.IsMissing() {
		return node
	}

	for idx := uint32(0); idx < node.ChildCount(); idx++ {
		child := node.Child(idx)
		if child.IsNull() || !child.HasError() {
			continue
		}

		return firstError(child)
	}

	return sitter.Node{}
}

func sourceLine(source []byte, offset uint) string {
	if len(source) == 0 {
		return ""
	}

	start := int(offset)
	if start > len(source) {
		start = len(source)
	}
	end := start

	for start > 0 && source[start-1] != '\n' && source[start-1] != '\r' {
		start--
	}
	for end < len(source) && source[end] != '\n' && source[end] != '\r' {
		end++
	}

	line := strings.TrimSpace(string(source[start:end]))
	if len(line) <= 96 {
		return line
	}

	return line[:96]
}
