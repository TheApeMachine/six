package synthesis

import (
	context "context"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/store/dmt"
	"github.com/theapemachine/six/pkg/validate"
)

/*
HASServer (Holographic Auto Synthesizer) is a system that can automatically
synthesize programs from data. It uses the system's native primitive
programmable Value type to construct "tools" on-the-fly in its
attempt to solve a given boundary value problem.
*/
type HASServer struct {
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	state      *errnie.State
	start      primitive.Value
	end        primitive.Value
	macroIndex *macro.MacroIndexServer
	forest     *dmt.Forest
}

/*
hasOpts configures HASServer at construction. Options inject context,
macro index, program server, or forest.
*/
type hasOpts func(*HASServer)

/*
NewHASServer creates the HAS server.
Default macro index and program server are created if not provided.
*/
func NewHASServer(options ...hasOpts) *HASServer {
	server := &HASServer{
		state: errnie.NewState("synthesis/has"),
	}

	for _, option := range options {
		option(server)
	}

	errnie.GuardVoid(server.state, func() error {
		return validate.Require(map[string]any{
			"ctx":    server.ctx,
			"cancel": server.cancel,
		})
	})

	if server.state.Failed() {
		return server
	}

	server.start = errnie.Guard(server.state, func() (primitive.Value, error) {
		return primitive.New()
	})

	server.end = errnie.Guard(server.state, func() (primitive.Value, error) {
		return primitive.New()
	})

	if server.state.Failed() {
		return server
	}

	return server
}

/*
Client returns a Cap'n Proto client for this HASServer.
*/
func (server *HASServer) Client(_ string) capnp.Client {
	server.mu.Lock()
	defer server.mu.Unlock()

	return capnp.Client(HAS_ServerToClient(server))
}

/*
Load reports no backlog signal yet for this server.
*/
func (server *HASServer) Load() int64 {
	return 0
}

/*
Close cancels the server context.
*/
func (server *HASServer) Close() error {
	server.mu.Lock()
	defer server.mu.Unlock()

	if server.cancel != nil {
		server.cancel()
	}

	return nil
}

/*
Write captures one ingestion boundary pair (start,end) for tool synthesis.
*/
func (server *HASServer) Write(ctx context.Context, call HAS_write) error {
	_ = ctx

	server.mu.Lock()
	defer server.mu.Unlock()

	server.state.Reset()

	start := errnie.Guard(server.state, func() (primitive.Value, error) {
		return call.Args().Start()
	})

	end := errnie.Guard(server.state, func() (primitive.Value, error) {
		return call.Args().End()
	})

	if server.state.Failed() {
		return server.state.Err()
	}

	server.copyDataIntoPrimitive(&server.start, start)
	server.copyDataIntoPrimitive(&server.end, end)

	return nil
}

/*
Done finalizes the latest ingestion pair by deriving/storing the affine tool.
*/
func (server *HASServer) Done(ctx context.Context, call HAS_done) error {
	_ = ctx

	server.mu.Lock()
	defer server.mu.Unlock()

	server.state.Reset()

	results, err := call.AllocResults()
	if err != nil {
		return err
	}

	if server.start.ActiveCount() == 0 || server.end.ActiveCount() == 0 {
		return NewHASError(HASErrorTypeStartAndEndRequired)
	}

	key, opcode, err := server.Derive(server.start, server.end)
	if err != nil {
		return err
	}

	if opcode == nil {
		return NewHASError(HASErrorTypeDeriveFailed)
	}

	if err := results.SetKeyText(key.String()); err != nil {
		return err
	}

	results.SetUseCount(opcode.UseCount)
	results.SetHardened(opcode.Hardened)
	results.SetWinnerIndex(-1)
	results.SetPostResidue(-1)
	results.SetSteps(0)

	return nil
}

/*
copyDataIntoPrimitive copies one data.Value block-for-block into an already
allocated primitive.Value buffer.
*/
func (server *HASServer) copyDataIntoPrimitive(dst *primitive.Value, value primitive.Value) {
	_ = server

	dst.SetC0(value.C0())
	dst.SetC1(value.C1())
	dst.SetC2(value.C2())
	dst.SetC3(value.C3())
	dst.SetC4(value.C4())
	dst.SetC5(value.C5())
	dst.SetC6(value.C6())
	dst.SetC7(value.C7())
}

/*
collectPromptBranches resolves all next-symbol branches for one prompt value.
*/
func (server *HASServer) collectPromptBranches(prompt primitive.Value) ([]primitive.Value, error) {
	if server.forest == nil {
		return nil, nil
	}

	promptData, err := primitive.New()
	if err != nil {
		return nil, err
	}

	promptData.SetC0(prompt.C0())
	promptData.SetC1(prompt.C1())
	promptData.SetC2(prompt.C2())
	promptData.SetC3(prompt.C3())
	promptData.SetC4(prompt.C4())
	promptData.SetC5(prompt.C5())
	promptData.SetC6(prompt.C6())
	promptData.SetC7(prompt.C7())

	promptSymbol, ok := primitive.InferLexicalSeed(promptData)
	if !ok {
		return nil, NewHASError(HASErrorTypePromptSymbolMissing)
	}

	coder := data.NewMortonCoder()
	symbolsByPosition := map[uint32]map[byte]struct{}{}

	server.forest.Iterate(func(keyBytes []byte, _ []byte) bool {
		if len(keyBytes) != 8 {
			return true
		}

		mortonKey := binary.BigEndian.Uint64(keyBytes)
		position, symbol := coder.Unpack(mortonKey)

		if _, exists := symbolsByPosition[position]; !exists {
			symbolsByPosition[position] = map[byte]struct{}{}
		}

		symbolsByPosition[position][symbol] = struct{}{}
		return true
	})

	branchSymbols := map[byte]struct{}{}

	for position, symbols := range symbolsByPosition {
		if _, exists := symbols[promptSymbol]; !exists {
			continue
		}

		nextSymbols, exists := symbolsByPosition[position+1]
		if !exists {
			continue
		}

		for symbol := range nextSymbols {
			branchSymbols[symbol] = struct{}{}
		}
	}

	if len(branchSymbols) == 0 {
		return nil, nil
	}

	orderedSymbols := make([]int, 0, len(branchSymbols))
	for symbol := range branchSymbols {
		orderedSymbols = append(orderedSymbols, int(symbol))
	}
	sort.Ints(orderedSymbols)

	branches := make([]primitive.Value, 0, len(orderedSymbols))
	for _, symbol := range orderedSymbols {
		value := primitive.BaseValue(byte(symbol))
		branch, err := primitive.New()
		if err != nil {
			return nil, err
		}

		branch.SetC0(value.C0())
		branch.SetC1(value.C1())
		branch.SetC2(value.C2())
		branch.SetC3(value.C3())
		branch.SetC4(value.C4())
		branch.SetC5(value.C5())
		branch.SetC6(value.C6())
		branch.SetC7(value.C7())
		branches = append(branches, branch)
	}

	return branches, nil
}

/*
HASOutcome captures one inference pass where a query mask reacts with a fact vat.
*/
type HASOutcome struct {
	QueryMask   primitive.Value
	Matches     []primitive.MatchResult
	WinnerIndex int
	Residue     primitive.Value
}

/*
Derive forges a reusable affine tool from one observed boundary pair and stores
it in the MacroIndex.
*/
func (server *HASServer) Derive(
	startValue primitive.Value,
	endValue primitive.Value,
) (macro.AffineKey, *macro.MacroOpcode, error) {
	if startValue.ActiveCount() == 0 || endValue.ActiveCount() == 0 {
		return macro.AffineKey{}, nil, NewHASError(HASErrorTypeStartAndEndRequired)
	}

	key := macro.AffineKeyFromValues(startValue, endValue)
	server.macroIndex.RecordOpcode(key)

	opcode, found := server.macroIndex.FindOpcode(key)

	if !found || opcode == nil {
		return macro.AffineKey{}, nil, NewHASError(HASErrorTypeDeriveFailed)
	}

	return key, opcode, nil
}

/*
Ask builds a reagent/query mask from known values and precipitates it against
candidate facts, returning the best zero-tension residue candidate.
*/
func (server *HASServer) Ask(
	knownValues []primitive.Value,
	vat []primitive.Value,
) (*HASOutcome, error) {
	if len(knownValues) == 0 {
		return nil, NewHASError(HASErrorTypeKnownValuesRequired)
	}

	if len(vat) == 0 {
		return nil, NewHASError(HASErrorTypeVatEmpty)
	}

	queryMask := primitive.BuildQueryMask(knownValues...)
	matches := make([]primitive.MatchResult, len(vat))

	bestIndex := 0
	bestAffinity := 0
	bestResidue := 0
	first := true

	for index := range vat {
		sharedBits, phaseQuotient, affinity, residueBits := primitive.ScoreMatch(queryMask, vat[index])

		matches[index].SharedBits = sharedBits
		matches[index].PhaseQuotient = phaseQuotient
		matches[index].FitnessScore = affinity

		if first {
			bestIndex = index
			bestAffinity = affinity
			bestResidue = residueBits
			first = false
			continue
		}

		if affinity > bestAffinity || (affinity == bestAffinity && residueBits < bestResidue) {
			bestIndex = index
			bestAffinity = affinity
			bestResidue = residueBits
		}
	}

	winner := queryMask.EvaluateMatch(vat[bestIndex])
	matches[bestIndex] = winner

	return &HASOutcome{
		QueryMask:   queryMask,
		Matches:     matches,
		WinnerIndex: bestIndex,
		Residue:     winner.Residue,
	}, nil
}

/*
HASWithContext injects context into HASServer.
*/
func HASWithContext(ctx context.Context) hasOpts {
	return func(server *HASServer) {
		server.ctx, server.cancel = context.WithCancel(ctx)
	}
}

/*
HASWithMacroIndex injects a shared MacroIndex for tool synthesis.
*/
func HASWithMacroIndex(index *macro.MacroIndexServer) hasOpts {
	return func(server *HASServer) {
		server.macroIndex = index
	}
}

/*
HASWithForest injects the shared DMT forest used for prompt branch lookups.
*/
func HASWithForest(forest *dmt.Forest) hasOpts {
	return func(server *HASServer) {
		server.forest = forest
	}
}

/*
HASErrorType enumerates typed HAS failure modes.
*/
type HASErrorType string

const (
	HASErrorTypeStartAndEndRequired   HASErrorType = "start and end values are required"
	HASErrorTypeDeriveFailed          HASErrorType = "failed to derive affine operator"
	HASErrorTypeKnownValuesRequired   HASErrorType = "known query values are required"
	HASErrorTypeVatEmpty              HASErrorType = "candidate vat cannot be empty"
	HASErrorTypePromptSymbolMissing   HASErrorType = "prompt lexical seed is required"
	HASErrorTypeProgramOutcomeMissing HASErrorType = "program execution produced no outcome"
)

/*
HASError is a typed error for HAS synthesis and inference failures.
*/
type HASError struct {
	Message string
	Err     HASErrorType
}

/*
NewHASError creates a typed HAS error from one failure code.
*/
func NewHASError(err HASErrorType) *HASError {
	return &HASError{Message: string(err), Err: err}
}

/*
Error implements error for HASError.
*/
func (err HASError) Error() string {
	return fmt.Sprintf("has error: %s: %s", err.Message, err.Err)
}
