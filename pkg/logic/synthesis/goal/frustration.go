package goal

import (
	context "context"
	"fmt"
	"math/rand"
	"net"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/logic/synthesis/bvp"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/validate"
)

/*
FrustrationEngineServer represents the "Phase-Locked Loop" logic solver.
It acts when a raw sequence fails to span a gap, causing Phase Tension (Frustration).
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
func (server *FrustrationEngineServer) Client(clientID string) Frustration {
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
Resolve evaluates the frustration (Phase Delta) between reality and belief.
If they don't match, it searches the MacroIndex for a sequential combination of tools
that zeroes the frustration. Returns the tool sequence to jump the span.
*/
func (fe *FrustrationEngineServer) Resolve(
	currentPhase numeric.Phase,
	targetPhase numeric.Phase,
	maxAttempts int,
) ([]*macro.MacroOpcode, error) {
	if currentPhase == targetPhase {
		// Zero frustration. Already locked.
		return nil, nil
	}

	if currentPhase == 0 || targetPhase == 0 {
		return nil, fmt.Errorf("phase cannot be zero")
	}

	// 1. Direct Resolution check (Cantilever)
	// If a single tool can bridge this gap exactly, use it.
	cl := bvp.NewCantileverServer(
		bvp.CantileverWithContext(fe.ctx),
		bvp.WithMacroIndex(fe.index),
	)
	rot, singleTool, err := cl.BridgePhases(currentPhase, targetPhase)

	if err == nil && singleTool != nil && singleTool.Hardened {
		return []*macro.MacroOpcode{singleTool}, nil
	}

	// Calculate the delta (frustration scalar for sorting/heuristics if we wanted)
	// Here, we just care if tension != 0 (i.e., state != target)

	// Fast path: get all hardened tools available to build a bridge
	tools := fe.index.AvailableHardened()
	if len(tools) == 0 {
		return nil, fmt.Errorf("no hardened tools available in library to relieve frustration gap")
	}

	delta := fe.calc.Subtract(targetPhase, currentPhase)
	// Deterministic PRNG seeded directly from the structural boundary problem
	prng := rand.New(rand.NewSource(int64(delta)))

	// 2. Sequential "Vibration" (Random Walk Composition)
	// Try random combination paths of tools until we hit target resonance
	for range maxAttempts {
		state := currentPhase
		var path []*macro.MacroOpcode

		// Try to bridge using a sequence of 1 to 3 tools
		numTools := prng.Intn(3) + 1
		for range numTools {
			// Pick a tool
			idx := prng.Intn(len(tools))
			tool := tools[idx]

			// Apply tool -- applying the logic circuit rotation (the scalar phase shift)
			state = fe.calc.Multiply(state, tool.Rotation)
			path = append(path, tool)

			if state == targetPhase {
				// Tension Zeroed! We discovered a composed logic circuit.
				// Package this sequence into the single needed rotation and record it.
				fe.index.RecordOpcode(rot)
				return path, nil
			}
		}
	}

	// Tension remains.
	return nil, fmt.Errorf("frustration engine failed to achieve phase-lock after %d attempts", maxAttempts)
}

/*
ResolveDual implements Multi-Headed Frustration (Dual-Goal Torsion).
When pulled by two conflicting logical targets, the GF(257) field enters Vector Torsion.
This method searches for a Cross-Domain Bridge—a composite rotation that satisfies a
hybrid of both targets, minimizing the combined shear stress.
*/
func (fe *FrustrationEngineServer) ResolveDual(
	currentPhase numeric.Phase,
	targetA numeric.Phase,
	targetB numeric.Phase,
	maxAttempts int,
) ([]*macro.MacroOpcode, error) {

	if currentPhase == targetA || currentPhase == targetB {
		return nil, nil // Already intersecting a goal
	}

	// 1. Calculate the Torsion (the conflicting mathematical pull)
	// We find a "Mean Rotation" or hybrid relaxation point in the field.
	// In GF(257), (A + B) / 2 is calculated as (A + B) * Inverse(2).
	sum := fe.calc.Add(targetA, targetB)
	inv2, _ := fe.calc.Inverse(2)
	hybridTarget := fe.calc.Multiply(sum, inv2)

	// In the event of a perfect 180-degree destructive cancellation modulo 257 yielding 0,
	// we fall back to a multiplicative geometric hybrid instead of an additive mean.
	if hybridTarget == 0 {
		hybridTarget = fe.calc.Add(fe.calc.Multiply(targetA, targetB), 1)
	}

	tools := fe.index.AvailableHardened()
	if len(tools) == 0 {
		return nil, fmt.Errorf("no hardened tools available for dual-goal torsion resolution")
	}

	delta := fe.calc.Subtract(hybridTarget, currentPhase)
	prng := rand.New(rand.NewSource(int64(delta))) // Deterministic seed based on Torsion

	// 2. Warp-Partitioned Search (Vibration towards the Hybrid)
	for range maxAttempts {
		state := currentPhase
		var path []*macro.MacroOpcode

		numTools := prng.Intn(4) + 1 // Allow slightly deeper composition for hybrids
		for range numTools {
			idx := prng.Intn(len(tools))
			tool := tools[idx]

			state = fe.calc.Multiply(state, tool.Rotation)
			path = append(path, tool)

			if state == hybridTarget {
				// We found a Cross-Domain Bridge!
				// We package the entire shift from current -> hybridTarget as a new MacroOpcode.
				// This effectively compresses the dual-goal tension into a single native logic gate.
				fe.index.RecordOpcode(hybridTarget)
				return path, nil
			}
		}
	}

	return nil, fmt.Errorf("dual-goal frustration failed to converge on a hybrid state after %d attempts", maxAttempts)
}
