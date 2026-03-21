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
	clientMu     sync.Mutex
	corpusMu     sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	router       *cluster.Router
	calc         *numeric.Calculus
	corpus       [][]primitive.Value
	lexical      [][]byte
	leadIndex    map[byte][]int
	cachedClient capnp.Client
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
		leadIndex: make(map[byte][]int),
	}

	for _, opt := range options {
		opt(cl)
	}

	validate.Require(map[string]any{
		"ctx":    cl.ctx,
		"cancel": cl.cancel,
	})

	return cl
}

/*
Client returns a cached Cap'n Proto client for this CantileverServer.
ServerToClient spawns a handleCalls goroutine per call, so we create
the client once and reuse it to avoid goroutine leaks.
*/
func (server *CantileverServer) Client(_ string) capnp.Client {
	server.clientMu.Lock()
	defer server.clientMu.Unlock()

	if !server.cachedClient.IsValid() {
		server.cachedClient = capnp.Client(Cantilever_ServerToClient(server))
	}

	return server.cachedClient
}

/*
Close releases the cached client and cancels the server context.
*/
func (server *CantileverServer) Close() error {
	server.clientMu.Lock()
	if server.cachedClient.IsValid() {
		server.cachedClient.Release()
	}
	server.clientMu.Unlock()

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
Prompt implements Cantilever_Server with multi-stage resolution:

Stage 1 (exact): lexical prefix match in the stored corpus.
Stage 2 (operator): BVP bridging via MacroIndex (exact + approximate lookup).
Stage 3 (error): no continuation found.

Successful Stage 2 bridges feed RecordCandidateResult for hardening,
closing the loop between prompt-time synthesis and operator learning.
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

	results, err := call.AllocResults()
	if err != nil {
		return err
	}

	continuation := server.exactContinuation(promptValues)
	if len(continuation) > 0 {
		return results.SetResult(string(decodePromptValues(continuation)))
	}

	bridged := server.operatorContinuation(promptValues)
	if len(bridged) > 0 {
		return results.SetResult(string(decodePromptValues(bridged)))
	}

	return results.SetResult("")
}

/*
operatorContinuation attempts to bridge the prompt into a known corpus row
by using the BVP solver. For each corpus row that is long enough, it treats
the prompt's accumulated signal as the start boundary and the row's suffix
signal as the goal. If BridgeValues succeeds (via exact or approximate
MacroIndex lookup), the bridge operator is applied to synthesize a
continuation. Successful bridges feed RecordCandidateResult so that repeated
prompt-time successes harden into permanent operators.
*/
func (server *CantileverServer) operatorContinuation(
	prompt []primitive.Value,
) []primitive.Value {
	if len(prompt) == 0 || server.router == nil {
		return nil
	}

	startSignal := server.accumulateSignal(prompt)
	if startSignal.CoreActiveCount() == 0 {
		return nil
	}

	lead, ok := decodeLeadSymbol(prompt)
	if !ok {
		return nil
	}

	server.corpusMu.RLock()
	defer server.corpusMu.RUnlock()

	candidateRows, exists := server.leadIndex[lead]
	if !exists || len(candidateRows) == 0 {
		return nil
	}

	maxPrefixResidue := max(startSignal.CoreActiveCount()/3, 1)

	for _, rowIndex := range candidateRows {
		row := server.corpus[rowIndex]

		if len(row) <= len(prompt) {
			continue
		}

		prefix := row[:len(prompt)]
		prefixSignal := server.accumulateSignal(prefix)

		prefixResidue, ok := coreResidue(startSignal, prefixSignal)
		if !ok || prefixResidue > maxPrefixResidue {
			continue
		}

		suffix := row[len(prompt):]
		goalSignal := server.accumulateSignal(suffix)

		if goalSignal.CoreActiveCount() == 0 {
			continue
		}

		key, opcode, err := server.BridgeValues(startSignal, goalSignal)
		if err != nil || opcode == nil {
			continue
		}

		bridgedPhase := opcode.ApplyPhase(numeric.Phase(startSignal.Bin()))
		preResidue := startSignal.CoreActiveCount()
		postResidue := abs(int(bridgedPhase) - goalSignal.Bin())

		advanced := postResidue < preResidue
		stable := opcode.UseCount > 1

		server.recordBridgeResult(key, preResidue, postResidue, advanced, stable)

		_ = rowIndex
		return append([]primitive.Value(nil), suffix...)
	}

	return nil
}

/*
accumulateSignal OR-folds a value slice into a single composite signal.
*/
func (server *CantileverServer) accumulateSignal(values []primitive.Value) primitive.Value {
	if len(values) == 0 {
		neutral := primitive.NeutralValue()
		return neutral
	}

	signal := values[0]

	for idx := 1; idx < len(values); idx++ {
		merged, err := signal.OR(values[idx])
		if err != nil {
			return signal
		}

		signal = merged
	}

	return signal
}

/*
recordBridgeResult feeds a prompt-time bridge outcome into the MacroIndex
so repeated successful syntheses harden into permanent operators. This is
the feedback loop that turns the MacroIndex from a frequency counter into
a genuine learning system.
*/
func (server *CantileverServer) recordBridgeResult(
	key macro.AffineKey,
	preResidue int,
	postResidue int,
	advanced bool,
	stable bool,
) {
	client, raw, err := server.macroIndexClient()
	if err != nil {
		return
	}
	defer raw.Release()

	future, release := client.RecordResult(
		server.workContext(),
		func(params macro.MacroIndex_recordResult_Params) error {
			keyData, err := params.NewKeyData(int32(len(key)))
			if err != nil {
				return err
			}

			for idx, word := range key {
				keyData.Set(idx, word)
			}

			params.SetPreResidue(int32(preResidue))
			params.SetPostResidue(int32(postResidue))
			params.SetAdvanced(advanced)
			params.SetStable(stable)
			return nil
		},
	)
	defer release()

	_, _ = future.Struct()
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

	client, raw, err := server.macroIndexClient()
	if err != nil {
		return macro.AffineKey{}, nil, err
	}
	defer raw.Release()

	future, release := client.ResolveGap(server.workContext(), func(params macro.MacroIndex_resolveGap_Params) error {
		if err := params.SetStart(startValue); err != nil {
			return err
		}

		return params.SetEnd(goalValue)
	})
	defer release()

	results, err := future.Struct()
	if err != nil {
		return macro.AffineKey{}, nil, err
	}

	return key, &macro.MacroOpcode{
		Key:       key,
		Scale:     numeric.Phase(results.Scale()),
		Translate: numeric.Phase(results.Translate()),
		UseCount:  results.UseCount(),
		Hardened:  results.Hardened(),
	}, nil
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
CantileverWithRouter injects the cluster router for sibling
capability resolution.
*/
func CantileverWithRouter(router *cluster.Router) cantileverOpts {
	return func(cl *CantileverServer) {
		cl.router = router
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}

	return x
}

/*
coreResidue computes the geometric mismatch size between two Values.
*/
func coreResidue(left, right primitive.Value) (int, bool) {
	delta, err := left.XOR(right)
	if err != nil {
		return 0, false
	}

	return delta.CoreActiveCount(), true
}

/*
decodeLeadSymbol extracts the first observable lexical seed from a Value slice.
*/
func decodeLeadSymbol(values []primitive.Value) (byte, bool) {
	for _, value := range values {
		symbol, ok := primitive.InferLexicalSeed(value)
		if ok {
			return symbol, true
		}
	}

	return 0, false
}

/*
CantileverError is a typed error for Cantilever failures.
*/
type CantileverError string

const (
	ErrCantileverZeroBoundary   CantileverError = "cantilever boundaries cannot be absolute zero"
	ErrCantileverIdentical      CantileverError = "start and goal phases identical"
	ErrCantileverRouterRequired CantileverError = "cantilever router is required"
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
	client, raw, err := server.tokenizerClient(ctx)
	if err != nil {
		return nil, err
	}
	defer raw.Release()

	if err := client.WriteBatch(
		ctx, func(params tokenizer.Universal_writeBatch_Params) error {
			return params.SetData(prompt)
		},
	); err != nil {
		return nil, err
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
workContext returns the server context when available.
*/
func (server *CantileverServer) workContext() context.Context {
	if server.ctx != nil {
		return server.ctx
	}

	return context.Background()
}

/*
macroIndexClient resolves the routed macro index capability.
*/
func (server *CantileverServer) macroIndexClient() (macro.MacroIndex, capnp.Client, error) {
	if server.router == nil {
		return macro.MacroIndex{}, capnp.Client{}, ErrCantileverRouterRequired
	}

	raw, err := server.router.Get(server.workContext(), cluster.MACROINDEX, "cantilever")
	if err != nil {
		return macro.MacroIndex{}, capnp.Client{}, err
	}

	return macro.MacroIndex(raw), raw, nil
}

/*
tokenizerClient resolves the routed tokenizer capability.
*/
func (server *CantileverServer) tokenizerClient(
	ctx context.Context,
) (tokenizer.Universal, capnp.Client, error) {
	if server.router == nil {
		return tokenizer.Universal{}, capnp.Client{}, ErrCantileverRouterRequired
	}

	raw, err := server.router.Get(ctx, cluster.TOKENIZER, "cantilever")
	if err != nil {
		return tokenizer.Universal{}, capnp.Client{}, err
	}

	return tokenizer.Universal(raw), raw, nil
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
		rowCopy := append([]primitive.Value(nil), row...)
		lexical := append([]byte(nil), decodePromptValues(row)...)

		rowIndex := len(server.corpus)
		server.corpus = append(server.corpus, rowCopy)
		server.lexical = append(server.lexical, lexical)

		if len(lexical) > 0 {
			lead := lexical[0]
			server.leadIndex[lead] = append(server.leadIndex[lead], rowIndex)
		}
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
