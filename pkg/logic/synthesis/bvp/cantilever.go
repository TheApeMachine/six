package bvp

import (
	"context"
	"fmt"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/system/cluster"
	"github.com/theapemachine/six/pkg/system/process/tokenizer"
	"github.com/theapemachine/six/pkg/validate"
)

/*
CantileverServer is a Boundary Value Problem (BVP)
synthesis engine. It uses phase mismatch between Start
and Goal points to drive a "Frustration Engine" that
synthesizes or discovers logical rotation tools that
map across the span.
*/
type CantileverServer struct {
	clientMu sync.Mutex
	corpusMu sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	// router is reserved for future cluster.Router-driven capability resolution
	// at synthesis boundaries (sibling services, load-aware peers).
	router  *cluster.Router
	calc    *numeric.Calculus
	Index   *macro.MacroIndexServer
	corpus  [][]primitive.Value
	lexical [][]byte
}

/*
cantileverOpts configures a CantileverServer
at construction.
*/
type cantileverOpts func(*CantileverServer)

/*
NewCantileverServer provides a new logic solver acting between fixed start and end boundary supports.
*/
func NewCantileverServer(options ...cantileverOpts) *CantileverServer {
	cl := &CantileverServer{
		calc:    numeric.NewCalculus(),
		corpus:  make([][]primitive.Value, 0),
		lexical: make([][]byte, 0),
	}

	for _, opt := range options {
		opt(cl)
	}

	validate.Require(map[string]any{
		"ctx":    cl.ctx,
		"cancel": cl.cancel,
	})

	if cl.Index == nil {
		cl.Index = macro.NewMacroIndexServer(
			macro.MacroIndexWithContext(cl.ctx),
		)
	}

	return cl
}

/*
Client returns a Cap'n Proto client for this CantileverServer.
*/
func (server *CantileverServer) Client(_ string) capnp.Client {
	server.clientMu.Lock()
	defer server.clientMu.Unlock()

	return capnp.Client(Cantilever_ServerToClient(server))
}

/*
Close cancels the server context.
*/
func (server *CantileverServer) Close() error {
	if server.cancel != nil {
		server.cancel()
	}

	return nil
}

/*
Load reports the relative execution pressure on the prompt solver.
*/
func (server *CantileverServer) Load() int64 {
	server.corpusMu.RLock()
	defer server.corpusMu.RUnlock()

	return int64(len(server.corpus))
}

/*
Prompt implements Cantilever_Server.
*/
func (server *CantileverServer) Prompt(ctx context.Context, call Cantilever_prompt) error {
	workCtx := server.ctx
	if workCtx == nil {
		workCtx = ctx
	}

	msg, err := call.Args().Msg()
	if err != nil {
		return err
	}

	promptValues, err := server.promptValues(workCtx, []byte(msg))
	if err != nil {
		return err
	}

	continuation := server.exactContinuation(promptValues)
	resultBytes := decodePromptValues(continuation)

	results, err := call.AllocResults()
	if err != nil {
		return err
	}

	return results.SetResult(string(resultBytes))
}

/*
Bridge implements Cantilever_Server. Accepts start and goal GF(257) phases via
RPC and computes the rotation needed to span the gap.
*/
func (server *CantileverServer) Bridge(ctx context.Context, call Cantilever_bridge) error {
	args := call.Args()

	startValue := primitive.BaseValue(byte(args.Start() % 256))
	startValue.SetStatePhase(numeric.Phase(args.Start()))

	goalValue := primitive.BaseValue(byte(args.Goal() % 256))
	goalValue.SetStatePhase(numeric.Phase(args.Goal()))

	_, opcode, err := server.BridgeValues(startValue, goalValue)
	if err != nil {
		return err
	}

	res, err := call.AllocResults()
	if err != nil {
		return err
	}

	if opcode != nil {
		res.SetRotation(uint32(opcode.Scale))
		res.SetHardened(opcode.Hardened)
	}

	return nil
}

/*
BridgeValues synthesizes a path between two 5-sparse Value states.
Computes the geometric AffineKey delta and looks up or records the
corresponding opcode in the MacroIndex. This is the sole solver
operating on the full 8.8 billion state space.
*/
func (server *CantileverServer) BridgeValues(
	startValue, goalValue primitive.Value,
) (macro.AffineKey, *macro.MacroOpcode, error) {

	if startValue.ActiveCount() == 0 || goalValue.ActiveCount() == 0 {
		return macro.AffineKey{}, nil, fmt.Errorf(
			"cantilever boundaries cannot have empty values",
		)
	}

	if startValue.CoreActiveCount() == goalValue.CoreActiveCount() {
		delta, err := startValue.XOR(goalValue)
		if err != nil {
			return macro.AffineKey{}, nil, err
		}

		if delta.CoreActiveCount() == 0 {
			return macro.AffineKey{}, nil, fmt.Errorf(
				"start and goal values identical, bridge span length is 0",
			)
		}
	}

	key := macro.AffineKeyFromValues(
		primitive.Value(startValue),
		primitive.Value(goalValue),
	)

	op, found := server.Index.FindOpcode(key)
	if found {
		server.Index.RecordOpcode(key)
		return key, op, nil
	}

	server.Index.RecordOpcode(key)
	opNew, _ := server.Index.FindOpcode(key)

	return key, opNew, nil
}

/*
CantileverWithContext sets the context.
*/
func CantileverWithContext(ctx context.Context) cantileverOpts {
	return func(cl *CantileverServer) {
		cl.ctx, cl.cancel = context.WithCancel(ctx)
	}
}

/*
WithMacroIndex injects a shared MacroIndex library to utilize discovered
Logic Circuits across Cantilever instances.
*/
func WithMacroIndex(index *macro.MacroIndexServer) cantileverOpts {
	return func(cl *CantileverServer) {
		cl.Index = index
	}
}

/*
CantileverWithRouter injects the cluster router for future sibling
capability resolution (see CantileverServer.router TODO on the field).
*/
func CantileverWithRouter(router *cluster.Router) cantileverOpts {
	return func(cl *CantileverServer) {
		cl.router = router
	}
}

/*
CantileverError is a typed error for Cantilever failures.
*/
type CantileverError string

const (
	ErrCantileverZeroBoundary CantileverError = "cantilever boundaries cannot be absolute zero"
	ErrCantileverIdentical    CantileverError = "start and goal phases identical"
)

/*
Error implements the error interface for CantileverError.
*/
func (cantileverError CantileverError) Error() string {
	return string(cantileverError)
}

/*
promptValues tokenizes prompt bytes and maps the keys into native Values.
*/
func (server *CantileverServer) promptValues(
	ctx context.Context,
	prompt []byte,
) ([]primitive.Value, error) {
	tokenizerServer := tokenizer.NewUniversalServer(
		tokenizer.UniversalWithContext(ctx),
	)
	defer tokenizerServer.Close()

	client := tokenizer.Universal(tokenizerServer.Client("cantilever"))

	for _, symbol := range prompt {
		if err := client.Write(
			ctx, func(params tokenizer.Universal_write_Params) error {
				params.SetData(symbol)
				return nil
			},
		); err != nil {
			return nil, err
		}
	}

	if err := client.WaitStreaming(); err != nil {
		return nil, err
	}

	keys, err := server.tokenizerKeys(ctx, client)
	if err != nil {
		return nil, err
	}

	return primitive.CompileObservableSequenceValues(keys), nil
}

/*
tokenizerKeys drains the tokenizer in the same two-pass pattern used by the VM.
*/
func (server *CantileverServer) tokenizerKeys(
	ctx context.Context,
	client tokenizer.Universal,
) ([]uint64, error) {
	return tokenizer.DrainKeys(ctx, client)
}

/*
decodePromptValues decodes lexical symbols from a continuation Value slice.
*/
func decodePromptValues(values []primitive.Value) []byte {
	return decodePromptValuesInfo(values).bytes
}

type promptDecodeInfo struct {
	bytes          []byte
	consumedValues int
}

func decodePromptValuesInfo(values []primitive.Value) promptDecodeInfo {
	out := make([]byte, 0, len(values))

	for _, value := range values {
		symbol, ok := primitive.InferLexicalSeed(value)
		if !ok {
			continue
		}

		out = append(out, symbol)
	}

	return promptDecodeInfo{
		bytes:          out,
		consumedValues: len(values),
	}
}

/*
Store appends dataset rows to the prompt corpus exactly as ingested.
*/
func (server *CantileverServer) Store(rows [][]primitive.Value) {
	server.corpusMu.Lock()
	defer server.corpusMu.Unlock()

	for _, row := range rows {
		server.corpus = append(server.corpus, append([]primitive.Value(nil), row...))
		server.lexical = append(server.lexical, append([]byte(nil), decodePromptValues(row)...))
	}
}

/*
exactContinuation returns the first exact corpus suffix for prompt.
*/
func (server *CantileverServer) exactContinuation(
	prompt []primitive.Value,
) []primitive.Value {
	server.corpusMu.RLock()
	defer server.corpusMu.RUnlock()

	promptInfo := decodePromptValuesInfo(prompt)
	promptBytes := promptInfo.bytes

	for index, row := range server.lexical {
		if len(row) <= len(promptBytes) {
			continue
		}

		if !server.hasExactPrefix(row, promptBytes) {
			continue
		}

		if len(server.corpus[index]) <= promptInfo.consumedValues {
			continue
		}

		return append([]primitive.Value(nil), server.corpus[index][promptInfo.consumedValues:]...)
	}

	return nil
}

/*
hasExactPrefix checks whether prompt matches the leading bytes in row exactly.
*/
func (server *CantileverServer) hasExactPrefix(
	row []byte,
	prompt []byte,
) bool {
	if len(prompt) == 0 || len(row) < len(prompt) {
		return false
	}

	for index := range prompt {
		if row[index] != prompt[index] {
			return false
		}
	}

	return true
}
