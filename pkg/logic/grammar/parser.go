package grammar

import (
	"fmt"
	"strings"

	"github.com/theapemachine/six/pkg/numeric"
)

/*
Parser maps NLP entities to mathematical geometry (GF(257)).
It treats Nouns as Points (States/Phases), Verbs as Vectors (Rotations),
and Adjectives as Nested Modifiers inside rotation functions.
*/
type Parser struct {
	calc *numeric.Calculus

	// Basic dictionaries for our "zero-shot" semantic grammar routing
	nounSet map[string]bool
	verbSet map[string]bool
	adjSet  map[string]bool
}

type parserOpts func(*Parser)

func NewParser(opts ...parserOpts) *Parser {
	p := &Parser{
		calc:    numeric.NewCalculus(),
		nounSet: make(map[string]bool),
		verbSet: make(map[string]bool),
		adjSet:  make(map[string]bool),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

/*
RegisterNoun declares a word as a geometric Point (Target State).
*/
func (p *Parser) RegisterNoun(words ...string) {
	for _, w := range words {
		p.nounSet[strings.ToLower(w)] = true
	}
}

/*
RegisterVerb declares a word as a geometric Vector (Phase Shift).
*/
func (p *Parser) RegisterVerb(words ...string) {
	for _, w := range words {
		p.verbSet[strings.ToLower(w)] = true
	}
}

/*
RegisterAdjective declares a word as a geometric Nested Rotation (Modifier).
*/
func (p *Parser) RegisterAdjective(words ...string) {
	for _, w := range words {
		p.adjSet[strings.ToLower(w)] = true
	}
}

/*
ASTNode represents a grammatical entity and its associated modifiers.
*/
type ASTNode struct {
	Entity    string
	Type      string // "noun" or "verb"
	Modifiers []string
}

/*
AST represents the parsed grammatical structure of a sentence (S-V-O).
*/
type AST struct {
	Subject ASTNode
	Verb    ASTNode
	Object  ASTNode
}

/*
ParseSentence builds an Abstract Syntax Tree (AST) from a basic S-V-O sentence
and transforms it into a GF(257) structural phase using non-commutative power rotations.
Requires: "[Adj] Noun [Adj] Verb [Adj] Noun".
*/
func (p *Parser) ParseSentence(sentence string) (*AST, numeric.Phase, error) {
	words := strings.Fields(strings.ToLower(sentence))
	if len(words) < 3 {
		return nil, 0, fmt.Errorf("sentence requires at least S-V-O structure (3 words)")
	}

	ast := &AST{}
	var currentMods []string
	
	// State machine: 0 = Subject, 1 = Verb, 2 = Object, 3 = Done
	state := 0 

	for _, w := range words {
		if p.adjSet[w] {
			currentMods = append(currentMods, w)
			continue
		}

		if p.nounSet[w] {
			if state == 0 {
				ast.Subject = ASTNode{Entity: w, Type: "noun", Modifiers: currentMods}
				currentMods = nil
				state = 1
				continue
			} else if state == 2 {
				ast.Object = ASTNode{Entity: w, Type: "noun", Modifiers: currentMods}
				currentMods = nil
				state = 3
				continue
			} else {
				return nil, 0, fmt.Errorf("unexpected noun '%s' found while looking for verb", w)
			}
		}

		if p.verbSet[w] {
			if state == 1 {
				ast.Verb = ASTNode{Entity: w, Type: "verb", Modifiers: currentMods}
				currentMods = nil
				state = 2
				continue
			} else {
				return nil, 0, fmt.Errorf("unexpected verb '%s' found in wrong structural position", w)
			}
		}

		return nil, 0, fmt.Errorf("unrecognized grammar entity: %s", w)
	}

	if state != 3 {
		return nil, 0, fmt.Errorf("incomplete sentence structure, failed to find subject, verb, and object")
	}

	if len(currentMods) > 0 {
		return nil, 0, fmt.Errorf("trailing modifiers with no noun/verb to attach to")
	}

	// Calculate Phase non-commutatively: State = State * G^(NodePhase)
	// This ensures "dog bites man" != "man bites dog"
	phase := numeric.Phase(1)
	
	phase = p.rotateNode(phase, ast.Subject)
	phase = p.rotateNode(phase, ast.Verb)
	phase = p.rotateNode(phase, ast.Object)

	return ast, phase, nil
}

/*
rotateNode cascades modifiers into the entity, then applies the block as a positional rotation
onto the base phase using a non-commutative polynomial sequence (State = State * 3 + Phase).
*/
func (p *Parser) rotateNode(base numeric.Phase, node ASTNode) numeric.Phase {
	// 1. Accumulate Modifiers non-commutatively inside the node
	nodePhase := p.calc.Sum(node.Entity)
	
	for _, mod := range node.Modifiers {
		// NodePhase = (NodePhase * 3) + ModPhase
		modPhase := p.calc.Sum(mod)
		nodePhase = p.calc.Add(p.calc.Multiply(nodePhase, 3), modPhase)
	}

	// 2. Rotate the Base Phase by the fully modified Entity
	// Base = (Base * 3) + NodePhase
	return p.calc.Add(p.calc.Multiply(base, 3), nodePhase)
}
