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
	Index       *macro.MacroIndexServer
}

/*
cantileverOpts configures a CantileverServer at construction.
*/
type cantileverOpts func(*CantileverServer)

/*
NewCantileverServer provides a new logic solver acting between fixed start and end boundary supports.
*/
func NewCantileverServer(options ...cantileverOpts) *CantileverServer {
	cl := &CantileverServer{
		clientConns: map[string]*rpc.Conn{},
		calc:        numeric.NewCalculus(),
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
Close shuts down the RPC connections and underlying net.Pipe,
unblocking goroutines stuck on pipe reads.
*/
func (server *CantileverServer) Close() error {
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
Prompt implements Cantilever_Server.
*/
func (server *CantileverServer) Prompt(ctx context.Context, call Cantilever_prompt) error {
	return nil
}

/*
Bridge implements Cantilever_Server. Accepts start and goal GF(257) phases via
RPC and computes the rotation needed to span the gap.
*/
func (server *CantileverServer) Bridge(ctx context.Context, call Cantilever_bridge) error {
	args := call.Args()

	startPhase := numeric.Phase(args.Start())
	goalPhase := numeric.Phase(args.Goal())

	rotation, opcode, err := server.BridgePhases(startPhase, goalPhase)
	if err != nil {
		return err
	}

	res, err := call.AllocResults()
	if err != nil {
		return err
	}

	res.SetRotation(uint32(rotation))

	if opcode != nil {
		res.SetHardened(opcode.Hardened)
	}

	return nil
}

/*
BridgePhases synthesizes a path between two GF(257) boundary phases.
It computes the Delta Rotation (G^X) necessary to transit the gap directly.
If standard raw navigation fails, it pulls a MacroOpcode or creates one.
*/
func (server *CantileverServer) BridgePhases(startPhase, goalPhase numeric.Phase) (numeric.Phase, *macro.MacroOpcode, error) {
	if startPhase == 0 || goalPhase == 0 {
		return 0, nil, fmt.Errorf("cantilever boundaries cannot be absolute zero")
	}

	if startPhase == goalPhase {
		return 0, nil, fmt.Errorf("start and goal phases identical, bridge span length is 0")
	}

	inverseStart, err := server.calc.Inverse(startPhase)
	if err != nil {
		return 0, nil, fmt.Errorf("could not compute inverse for cantilever start boundary: %w", err)
	}

	targetRotation := server.calc.Multiply(goalPhase, inverseStart)

	op, found := server.Index.FindOpcode(targetRotation)
	if found {
		server.Index.RecordOpcode(targetRotation)
		return targetRotation, op, nil
	}

	server.Index.RecordOpcode(targetRotation)
	opNew, _ := server.Index.FindOpcode(targetRotation)

	return targetRotation, opNew, nil
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
