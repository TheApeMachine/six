package bvp

import (
	"context"
	"fmt"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/system/cluster"
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
	ctx    context.Context
	cancel context.CancelFunc
	router *cluster.Router
	calc   *numeric.Calculus
	Index  *macro.MacroIndexServer
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
		calc: numeric.NewCalculus(),
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
CantileverWithRouter injects the cluster router so the cantilever can
resolve sibling capabilities at call time.
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
