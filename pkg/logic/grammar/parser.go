package grammar

import (
	"context"
	"net"
	"strings"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/validate"
)

/*
ParserServer maps NLP entities to mathematical geometry (GF(257)).
It treats Nouns as Points (States/Phases), Verbs as Vectors (Rotations),
and Adjectives as Nested Modifiers inside rotation functions.
*/
type ParserServer struct {
	ctx         context.Context
	cancel      context.CancelFunc
	serverSide  net.Conn
	clientSide  net.Conn
	client      Parser
	serverConn  *rpc.Conn
	clientConns map[string]*rpc.Conn
	calc        *numeric.Calculus
	nounSet     map[string]bool
	verbSet     map[string]bool
	adjSet      map[string]bool
	stopSet     map[string]bool
}

type parserOpts func(*ParserServer)

func NewParserServer(opts ...parserOpts) *ParserServer {
	p := &ParserServer{
		clientConns: map[string]*rpc.Conn{},
		calc:        numeric.NewCalculus(),
		nounSet:     make(map[string]bool),
		verbSet:     make(map[string]bool),
		adjSet:      make(map[string]bool),
		stopSet:     make(map[string]bool),
	}

	for _, opt := range opts {
		opt(p)
	}

	validate.Require(map[string]any{
		"ctx":    p.ctx,
		"cancel": p.cancel,
	})

	p.serverSide, p.clientSide = net.Pipe()
	p.client = Parser_ServerToClient(p)

	p.serverConn = rpc.NewConn(rpc.NewStreamTransport(
		p.serverSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(p.client),
	})

	return p
}

/*
Client returns a Cap'n Proto client connected to this ParserServer.
*/
func (server *ParserServer) Client(clientID string) Parser {
	server.clientConns[clientID] = rpc.NewConn(rpc.NewStreamTransport(
		server.clientSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(server.client),
	})

	return server.client
}

/*
Close shuts down the RPC connections and underlying net.Pipe,
unblocking goroutines stuck on pipe reads.
*/
func (server *ParserServer) Close() error {
	if server.serverConn != nil {
		_ = server.serverConn.Close()
		server.serverConn = nil
	}

	for clientID, conn := range server.clientConns {
		if conn != nil {
			_ = conn.Close()
		}
		delete(server.clientConns, clientID)
	}

	if server.serverSide != nil {
		_ = server.serverSide.Close()
		server.serverSide = nil
	}
	if server.clientSide != nil {
		_ = server.clientSide.Close()
		server.clientSide = nil
	}
	if server.cancel != nil {
		server.cancel()
	}

	return nil
}

/*
Prompt implements Parser_Server.
*/
func (server *ParserServer) Prompt(ctx context.Context, call Parser_prompt) error {
	return nil
}

/*
Parse implements Parser_Server. Decomposes a prompt string into its S-V-O
AST and returns the computed GF(257) phase along with the extracted components.
*/
func (server *ParserServer) Parse(ctx context.Context, call Parser_parse) error {
	args := call.Args()

	msg, err := args.Msg()
	if err != nil {
		return err
	}

	ast, phase, parseErr := server.ParseSentence(msg)

	res, err := call.AllocResults()
	if err != nil {
		return err
	}

	if parseErr != nil {
		res.SetPhase(0)
		return nil
	}

	res.SetPhase(uint32(phase))

	if err := res.SetSubject(ast.Subject.Entity); err != nil {
		return err
	}

	if err := res.SetVerb(ast.Verb.Entity); err != nil {
		return err
	}

	return res.SetObject(ast.Object.Entity)
}

/*
ParserWithContext sets the context.
*/
func ParserWithContext(ctx context.Context) parserOpts {
	return func(p *ParserServer) {
		p.ctx, p.cancel = context.WithCancel(ctx)
	}
}

/*
ParserWithNouns registers noun vocabulary at construction time.
*/
func ParserWithNouns(words ...string) parserOpts {
	return func(p *ParserServer) {
		p.RegisterNoun(words...)
	}
}

/*
ParserWithVerbs registers verb vocabulary at construction time.
*/
func ParserWithVerbs(words ...string) parserOpts {
	return func(p *ParserServer) {
		p.RegisterVerb(words...)
	}
}

/*
ParserWithAdjectives registers adjective vocabulary at construction time.
*/
func ParserWithAdjectives(words ...string) parserOpts {
	return func(p *ParserServer) {
		p.RegisterAdjective(words...)
	}
}

/*
ParserWithStopwords registers words to silently skip during parsing.
*/
func ParserWithStopwords(words ...string) parserOpts {
	return func(p *ParserServer) {
		for _, w := range words {
			p.stopSet[strings.ToLower(w)] = true
		}
	}
}

/*
RegisterNoun declares a word as a geometric Point (Target State).
*/
func (p *ParserServer) RegisterNoun(words ...string) {
	for _, w := range words {
		p.nounSet[strings.ToLower(w)] = true
	}
}

/*
RegisterVerb declares a word as a geometric Vector (Phase Shift).
*/
func (p *ParserServer) RegisterVerb(words ...string) {
	for _, w := range words {
		p.verbSet[strings.ToLower(w)] = true
	}
}

/*
RegisterAdjective declares a word as a geometric Nested Rotation (Modifier).
*/
func (p *ParserServer) RegisterAdjective(words ...string) {
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
Stopwords are silently skipped. Requires S-V-O structure from the remaining tokens.
*/
func (p *ParserServer) ParseSentence(sentence string) (*AST, numeric.Phase, error) {
	raw := strings.FieldsFunc(strings.ToLower(sentence), func(r rune) bool {
		return r == ' ' || r == '.' || r == '?' || r == '!' || r == ','
	})

	var words []string

	for _, w := range raw {
		if !p.stopSet[w] {
			words = append(words, w)
		}
	}

	if len(words) < 3 {
		return nil, 0, ParserError("sentence requires at least S-V-O structure (3 words)")
	}

	ast := &AST{}
	var currentMods []string

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
				return nil, 0, ParserError("unexpected noun in verb position: " + w)
			}
		}

		if p.verbSet[w] {
			if state == 1 {
				ast.Verb = ASTNode{Entity: w, Type: "verb", Modifiers: currentMods}
				currentMods = nil
				state = 2
				continue
			} else {
				return nil, 0, ParserError("unexpected verb in wrong position: " + w)
			}
		}

		return nil, 0, ParserError("unrecognized grammar entity: " + w)
	}

	if state != 3 {
		return nil, 0, ParserError("incomplete sentence structure, failed to find subject, verb, and object")
	}

	if len(currentMods) > 0 {
		return nil, 0, ParserError("trailing modifiers with no noun/verb to attach to")
	}

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
func (p *ParserServer) rotateNode(base numeric.Phase, node ASTNode) numeric.Phase {
	nodePhase := p.calc.Sum(node.Entity)

	for _, mod := range node.Modifiers {
		modPhase := p.calc.Sum(mod)
		nodePhase = p.calc.Add(p.calc.Multiply(nodePhase, 3), modPhase)
	}

	return p.calc.Add(p.calc.Multiply(base, 3), nodePhase)
}

/*
PromptToGrammar implements the full API pipeline for Language parsing.
*/
func (p *ParserServer) PromptToGrammar(prompt string) (*AST, numeric.Phase, error) {
	return p.ParseSentence(prompt)
}

/*
ParserError is a typed error for Parser failures.
*/
type ParserError string

/*
Error implements the error interface for ParserError.
*/
func (parserError ParserError) Error() string {
	return string(parserError)
}
