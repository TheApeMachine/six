package bvp

import (
	"context"
	"fmt"
	"net"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/validate"
)

/*
CantileverServer is a Boundary Value Problem (BVP) synthesis engine.
It uses phase mismatch between Start and Goal points to drive a "Frustration Engine"
that synthesizes or discovers logical rotation tools that map across the span.
*/
type CantileverServer struct {
	ctx         context.Context
	cancel      context.CancelFunc
	serverSide  net.Conn
	clientSide  net.Conn
	client      Cantilever
	serverConn  *rpc.Conn
	clientConns map[string]*rpc.Conn
	calc        *numeric.Calculus
	StartPhase  numeric.Phase
	GoalPhase   numeric.Phase
	Index       *macro.MacroIndexServer
}

/*
opts ...
*/
type cantileverOpts func(*CantileverServer)

/*
NewCantilever provides a new logic solver acting between fixed start and end boundary supports.
*/
func NewCantileverServer(start, goal numeric.Phase, options ...cantileverOpts) *CantileverServer {
	cl := &CantileverServer{
		clientConns: map[string]*rpc.Conn{},
		calc:        numeric.NewCalculus(),
		StartPhase:  start,
		GoalPhase:   goal,
		Index:       macro.NewMacroIndexServer(),
	}

	for _, opt := range options {
		opt(cl)
	}

	validate.Require(map[string]any{
		"ctx":    cl.ctx,
		"cancel": cl.cancel,
	})

	cl.serverSide, cl.clientSide = net.Pipe()
	cl.client = Cantilever_ServerToClient(cl)

	cl.serverConn = rpc.NewConn(rpc.NewStreamTransport(
		cl.serverSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(cl.client),
	})

	return cl
}

/*
Client returns a Cap'n Proto client connected to this CantileverServer.
*/
func (server *CantileverServer) Client(clientID string) Cantilever {
	server.clientConns[clientID] = rpc.NewConn(rpc.NewStreamTransport(
		server.clientSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(server.client),
	})

	return server.client
}

/*
Prompt implements Cantilever_Server.
*/
func (server *CantileverServer) Prompt(ctx context.Context, call Cantilever_prompt) error {
	return nil
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
Bridge synthesizes a path between the Start and Goal boundaries.
It computes the Delta Rotation (G^X) necessary to transit the gap directly.
If standard raw navigation fails, it pulls a MacroOpcode or creates one.
*/
func (cl *CantileverServer) Bridge() (numeric.Phase, *macro.MacroOpcode, error) {
	if cl.StartPhase == 0 || cl.GoalPhase == 0 {
		return 0, nil, fmt.Errorf("cantilever boundaries cannot be absolute zero")
	}

	if cl.StartPhase == cl.GoalPhase {
		return 0, nil, fmt.Errorf("start and goal phases identical, bridge span length is 0")
	}

	// Calculate the necessary Phase Shift (Tool Rotation) to bridge the gap:
	// Rot = (Goal Phase * Inverse(Start Phase)) % 257
	inverseStart, err := cl.calc.Inverse(cl.StartPhase)
	if err != nil {
		return 0, nil, fmt.Errorf("could not compute inverse for cantilever start boundary: %w", err)
	}
	targetRotation := cl.calc.Multiply(cl.GoalPhase, inverseStart)

	// Step 1: Scan library for a known tool capable of bridging this span
	op, found := cl.Index.FindOpcode(targetRotation)
	if found {
		// Tool found. We successfully span the gap using pre-synthesized logic constraints.
		cl.Index.RecordOpcode(targetRotation) // Increment usage
		return targetRotation, op, nil
	}

	// Step 2: The Cantilever fails via Frustration (no known tool can bridge).
	// We synthesize a generalized patch for this Delta and "Harden" it.
	cl.Index.RecordOpcode(targetRotation) // Synthesize new!
	opNew, _ := cl.Index.FindOpcode(targetRotation)

	return targetRotation, opNew, nil
}
