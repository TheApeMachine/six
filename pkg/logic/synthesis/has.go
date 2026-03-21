package synthesis

import (
	context "context"
	"fmt"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
	dmtserver "github.com/theapemachine/six/pkg/store/dmt/server"
	"github.com/theapemachine/six/pkg/system/cluster"
	"github.com/theapemachine/six/pkg/validate"
)

/*
HASServer (Holographic Auto Synthesizer) is a system that can automatically
synthesize programs from data. It uses the system's native primitive
programmable Value type to construct "tools" on-the-fly in its
attempt to solve a given boundary value problem.
*/
type HASServer struct {
	mu           sync.RWMutex
	clientMu     sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	state        *errnie.State
	router       *cluster.Router
	start        primitive.Value
	end          primitive.Value
	cachedClient capnp.Client
}

/*
hasOpts configures HASServer with context and routed capability access.
*/
type hasOpts func(*HASServer)

/*
NewHASServer creates the HAS server.
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
Client returns a cached Cap'n Proto client for this HASServer.
ServerToClient spawns a handleCalls goroutine per call, so we create
the client once and reuse it to avoid goroutine leaks.
*/
func (server *HASServer) Client(_ string) capnp.Client {
	server.clientMu.Lock()
	defer server.clientMu.Unlock()

	if !server.cachedClient.IsValid() {
		server.cachedClient = capnp.Client(HAS_ServerToClient(server))
	}

	return server.cachedClient
}

/*
Load derives a lightweight pressure signal from the staged boundary pair.
*/
func (server *HASServer) Load() int64 {
	server.mu.RLock()
	defer server.mu.RUnlock()

	return int64(server.start.ActiveCount() + server.end.ActiveCount())
}

/*
Close releases the cached client and cancels the server context.
*/
func (server *HASServer) Close() error {
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
	client, raw, err := server.forestClient()
	if err != nil {
		return nil, err
	}
	defer raw.Release()

	future, release := client.Branches(server.workContext(), func(params dmtserver.Server_branches_Params) error {
		return params.SetPrompt(prompt)
	})
	defer release()

	results, err := future.Struct()
	if err != nil {
		return nil, err
	}

	branchList, err := results.Branches()
	if err != nil {
		return nil, err
	}

	branches := make([]primitive.Value, 0, branchList.Len())
	for index := 0; index < branchList.Len(); index++ {
		branch := branchList.At(index)
		cloned, cloneErr := primitive.New()
		if cloneErr != nil {
			return nil, cloneErr
		}

		server.copyDataIntoPrimitive(&cloned, branch)
		branches = append(branches, cloned)
	}

	return branches, nil
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
	client, raw, err := server.macroIndexClient()
	if err != nil {
		return macro.AffineKey{}, nil, err
	}
	defer raw.Release()

	future, release := client.ResolveGap(server.workContext(), func(params macro.MacroIndex_resolveGap_Params) error {
		if err := params.SetStart(startValue); err != nil {
			return err
		}

		return params.SetEnd(endValue)
	})
	defer release()

	result, err := future.Struct()
	if err != nil {
		return macro.AffineKey{}, nil, err
	}

	opcode := &macro.MacroOpcode{
		Key:       key,
		Scale:     numeric.Phase(result.Scale()),
		Translate: numeric.Phase(result.Translate()),
		UseCount:  result.UseCount(),
		Hardened:  result.Hardened(),
	}

	return key, opcode, nil
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
HASWithRouter injects the cluster router for sibling capability resolution.
*/
func HASWithRouter(router *cluster.Router) hasOpts {
	return func(server *HASServer) {
		server.router = router
	}
}

/*
workContext returns the server context when available.
*/
func (server *HASServer) workContext() context.Context {
	if server.ctx != nil {
		return server.ctx
	}

	return context.Background()
}

/*
macroIndexClient resolves the routed macro index capability.
*/
func (server *HASServer) macroIndexClient() (macro.MacroIndex, capnp.Client, error) {
	if server.router == nil {
		return macro.MacroIndex{}, capnp.Client{}, NewHASError(HASErrorTypeRouterRequired)
	}

	raw, err := server.router.Get(server.workContext(), cluster.MACROINDEX, "has")
	if err != nil {
		return macro.MacroIndex{}, capnp.Client{}, err
	}

	return macro.MacroIndex(raw), raw, nil
}

/*
forestClient resolves the routed forest capability.
*/
func (server *HASServer) forestClient() (dmtserver.Server, capnp.Client, error) {
	if server.router == nil {
		return dmtserver.Server{}, capnp.Client{}, NewHASError(HASErrorTypeRouterRequired)
	}

	raw, err := server.router.Get(server.workContext(), cluster.FOREST, "has")
	if err != nil {
		return dmtserver.Server{}, capnp.Client{}, err
	}

	return dmtserver.Server(raw), raw, nil
}

/*
HASErrorType enumerates typed HAS failure modes.
*/
type HASErrorType string

const (
	HASErrorTypeStartAndEndRequired   HASErrorType = "start and end values are required"
	HASErrorTypeDeriveFailed          HASErrorType = "failed to derive affine operator"
	HASErrorTypePromptSymbolMissing   HASErrorType = "prompt lexical seed is required"
	HASErrorTypeProgramOutcomeMissing HASErrorType = "program execution produced no outcome"
	HASErrorTypeRouterRequired        HASErrorType = "router is required"
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
