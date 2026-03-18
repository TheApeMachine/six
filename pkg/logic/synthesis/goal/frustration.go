package goal

import (
	context "context"
	"fmt"
	"net"
	"sort"
	"strings"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/logic/synthesis/bvp"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/validate"
)

/*
FrustrationEngineServer represents the geometric logic solver.
It acts when a raw sequence fails to span a gap, causing geometric tension.
The Engine vibrates the MacroIndex, applying discovered logic tools until the tension zeros out.
*/
type FrustrationEngineServer struct {
	ctx         context.Context
	cancel      context.CancelFunc
	serverSide  net.Conn
	clientSide  net.Conn
	client      Frustration
	serverConn  *rpc.Conn
	clientConns map[string]*rpc.Conn
	calc        *numeric.Calculus
	index       *macro.MacroIndexServer
}

/*
feOpts configuration for FrustrationEngine.
*/
type feOpts func(*FrustrationEngineServer)

/*
NewFrustrationEngineServer instantiates the tension-relieving logic solver.
*/
func NewFrustrationEngineServer(opts ...feOpts) *FrustrationEngineServer {
	fe := &FrustrationEngineServer{
		clientConns: map[string]*rpc.Conn{},
		calc:        numeric.NewCalculus(),
	}

	for _, opt := range opts {
		opt(fe)
	}

	validate.Require(map[string]any{
		"ctx":    fe.ctx,
		"cancel": fe.cancel,
	})

	if fe.index == nil {
		fe.index = macro.NewMacroIndexServer(
			macro.MacroIndexWithContext(fe.ctx),
		)
	}

	fe.serverSide, fe.clientSide = net.Pipe()
	fe.client = Frustration_ServerToClient(fe)

	fe.serverConn = rpc.NewConn(rpc.NewStreamTransport(
		fe.serverSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(fe.client),
	})

	return fe
}

/*
Client returns a Cap'n Proto client connected to this FrustrationEngineServer.
*/
func (server *FrustrationEngineServer) Client(clientID string) *Frustration {
	server.clientConns[clientID] = rpc.NewConn(rpc.NewStreamTransport(
		server.clientSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(server.client),
	})

	return &server.client
}

/*
Close shuts down the RPC connections and underlying net.Pipe,
unblocking goroutines stuck on pipe reads.
*/
func (server *FrustrationEngineServer) Close() error {
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
Prompt implements Frustration_Server.
*/
func (server *FrustrationEngineServer) Prompt(ctx context.Context, call Frustration_prompt) error {
	return nil
}

/*
FrustrationWithContext sets the context.
*/
func FrustrationWithContext(ctx context.Context) feOpts {
	return func(fe *FrustrationEngineServer) {
		fe.ctx, fe.cancel = context.WithCancel(ctx)
	}
}

/*
WithSharedIndex allows the Frustration Engine to pull from a global Library of OpCodes.
*/
func WithSharedIndex(index *macro.MacroIndexServer) feOpts {
	return func(fe *FrustrationEngineServer) {
		fe.index = index
	}
}

/*
ResolveCandidates deterministically enumerates up to maxResults hardened tool paths
that bridge currentValue to targetValue. Results are ordered by path length first,
then by accumulated support in the macro library.
*/
func (fe *FrustrationEngineServer) ResolveCandidates(
	currentValue data.Value,
	targetValue data.Value,
	maxAttempts int,
	maxResults int,
) ([][]*macro.MacroOpcode, error) {
	return fe.resolveCandidatesToTarget(
		currentValue,
		targetValue,
		maxAttempts,
		maxResults,
		fe.candidateDepth(maxAttempts),
	)
}

func (fe *FrustrationEngineServer) candidateDepth(maxAttempts int) int {
	switch {
	case maxAttempts >= 10000:
		return 5
	case maxAttempts >= 1000:
		return 4
	case maxAttempts >= 64:
		return 3
	default:
		return 2
	}
}

func (fe *FrustrationEngineServer) availableHardenedSorted() []*macro.MacroOpcode {
	tools := fe.index.AvailableHardened()
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].UseCount != tools[j].UseCount {
			return tools[i].UseCount > tools[j].UseCount
		}
		return tools[i].Scale < tools[j].Scale
	})
	return tools
}

func cloneMacroPath(path []*macro.MacroOpcode) []*macro.MacroOpcode {
	if len(path) == 0 {
		return nil
	}
	return append([]*macro.MacroOpcode(nil), path...)
}

func macroPathSignature(path []*macro.MacroOpcode) string {
	var builder strings.Builder
	for i, opcode := range path {
		if i > 0 {
			builder.WriteByte('-')
		}
		builder.WriteString(opcode.Key.String())
	}
	return builder.String()
}

func macroPathSupport(path []*macro.MacroOpcode) uint64 {
	var support uint64
	for _, opcode := range path {
		support += opcode.UseCount
	}
	return support
}

func (fe *FrustrationEngineServer) resolveCandidatesToTarget(
	currentValue data.Value,
	targetValue data.Value,
	maxAttempts int,
	maxResults int,
	maxDepth int,
) ([][]*macro.MacroOpcode, error) {
	if currentValue.ActiveCount() == 0 || targetValue.ActiveCount() == 0 {
		return nil, fmt.Errorf("values cannot be empty")
	}

	delta := currentValue.XOR(targetValue)
	if delta.CoreActiveCount() == 0 {
		return nil, nil
	}

	if maxResults <= 0 {
		maxResults = 1
	}
	if maxDepth <= 0 {
		maxDepth = 1
	}
	if maxAttempts <= 0 {
		maxAttempts = 64
	}

	results := make([][]*macro.MacroOpcode, 0, maxResults)
	seen := make(map[string]bool, maxResults)
	addCandidate := func(path []*macro.MacroOpcode) {
		if len(path) == 0 {
			return
		}
		signature := macroPathSignature(path)
		if seen[signature] {
			return
		}
		seen[signature] = true
		results = append(results, cloneMacroPath(path))
	}

	cl := bvp.NewCantileverServer(
		bvp.CantileverWithContext(fe.ctx),
		bvp.WithMacroIndex(fe.index),
	)

	targetKey := macro.AffineKeyFromValues(currentValue, targetValue)

	if _, singleTool, err := cl.BridgeValues(currentValue, targetValue); err == nil && singleTool != nil && singleTool.Hardened {
		nextState := currentValue.ApplyAffineValue(singleTool.Scale, singleTool.Translate)
		if macro.AffineKeyFromValues(currentValue, nextState) == targetKey {
			addCandidate([]*macro.MacroOpcode{singleTool})
		}
	}

	tools := fe.availableHardenedSorted()
	if len(tools) == 0 {
		if len(results) == 0 {
			return nil, fmt.Errorf("no hardened tools available in library to relieve frustration gap")
		}
		return results, nil
	}

	budget := maxAttempts
	var walk func(state data.Value, depth int, path []*macro.MacroOpcode)
	walk = func(state data.Value, depth int, path []*macro.MacroOpcode) {
		if budget <= 0 || len(results) >= maxResults || depth >= maxDepth {
			return
		}

		for _, tool := range tools {
			if budget <= 0 || len(results) >= maxResults {
				return
			}
			budget--

			nextState := state.ApplyAffineValue(tool.Scale, tool.Translate)
			nextPath := append(cloneMacroPath(path), tool)

			nextKey := macro.AffineKeyFromValues(currentValue, nextState)
			if nextKey == targetKey {
				addCandidate(nextPath)
				continue
			}

			if depth+1 < maxDepth {
				walk(nextState, depth+1, nextPath)
			}
		}
	}
	walk(currentValue, 0, nil)

	sort.Slice(results, func(i, j int) bool {
		if len(results[i]) != len(results[j]) {
			return len(results[i]) < len(results[j])
		}

		supportI := macroPathSupport(results[i])
		supportJ := macroPathSupport(results[j])
		if supportI != supportJ {
			return supportI > supportJ
		}

		return macroPathSignature(results[i]) < macroPathSignature(results[j])
	})

	if len(results) == 0 {
		return nil, fmt.Errorf("frustration engine failed to achieve phase-lock after %d attempts", maxAttempts)
	}

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results, nil
}

/*
Resolve evaluates the frustration (geometric delta) between reality and belief.
If they don't match, it searches the MacroIndex for a sequential combination of tools
that zeroes the frustration. Returns the tool sequence to jump the span.
*/
func (fe *FrustrationEngineServer) Resolve(
	currentValue data.Value,
	targetValue data.Value,
	maxAttempts int,
) ([]*macro.MacroOpcode, error) {
	candidates, err := fe.ResolveCandidates(currentValue, targetValue, maxAttempts, 1)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	key := macro.AffineKeyFromValues(currentValue, targetValue)
	fe.index.RecordOpcode(key)

	return candidates[0], nil
}

/*
ResolveDual implements Multi-Headed Frustration (Dual-Goal Torsion).
When pulled by two conflicting logical targets, the system enters geometric torsion.
This method searches for a cross-domain bridge that satisfies a hybrid of both targets.
*/
func (fe *FrustrationEngineServer) ResolveDual(
	currentValue data.Value,
	targetA data.Value,
	targetB data.Value,
	maxAttempts int,
) ([]*macro.MacroOpcode, error) {
	deltaA := currentValue.XOR(targetA)
	deltaB := currentValue.XOR(targetB)

	if deltaA.CoreActiveCount() == 0 {
		return nil, nil
	}
	if deltaB.CoreActiveCount() == 0 {
		return nil, nil
	}

	hybridTarget := targetA.XOR(targetB)
	hybridTarget = currentValue.XOR(hybridTarget)

	if hybridTarget.ActiveCount() == 0 {
		hybridTarget = targetA
	}

	candidates, err := fe.resolveCandidatesToTarget(currentValue, hybridTarget, maxAttempts, 1, 4)
	if err != nil {
		return nil, fmt.Errorf("dual-goal frustration failed to converge on a hybrid state after %d attempts", maxAttempts)
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("dual-goal frustration failed to converge on a hybrid state after %d attempts", maxAttempts)
	}

	key := macro.AffineKeyFromValues(currentValue, hybridTarget)
	fe.index.RecordOpcode(key)

	return candidates[0], nil
}
