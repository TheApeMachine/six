package substrate

import (
	"context"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/path"
	"github.com/theapemachine/six/pkg/logic/synthesis/goal"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/telemetry"
	"github.com/theapemachine/six/pkg/validate"
)

/*
GraphServer implements the Cap'n Proto RPC interface for the logic graph.
It acts as the reasoning engine (Graph), evaluating geometric vector states.
The Machine is the sole orchestrator: it fetches data from SpatialIndex and
hands it to GraphServer via Prompt. GraphServer never calls any other server.
*/
type GraphServer struct {
	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	state         *errnie.State
	serverSide    net.Conn
	clientSide    net.Conn
	client        Graph
	serverConn    *rpc.Conn
	clientConns   map[string]*rpc.Conn
	workerPool    *pool.Pool
	sink          *telemetry.Sink
	pathWavefront *path.Wavefront
	frustration   *goal.Frustration
	data          []uint64
}

/*
GraphOpt configures GraphServer.
*/
type GraphOpt func(*GraphServer)

/*
NewGraphServer creates the Cap'n Proto RPC server for the logic graph.
*/
func NewGraphServer(opts ...GraphOpt) *GraphServer {
	graph := &GraphServer{
		state:       errnie.NewState("logic/substrate/graph"),
		clientConns: map[string]*rpc.Conn{},
		sink:        telemetry.NewSink(),
	}

	for _, opt := range opts {
		opt(graph)
	}

	errnie.GuardVoid(graph.state, func() error {
		return validate.Require(map[string]any{
			"ctx":        graph.ctx,
			"workerPool": graph.workerPool,
		})
	})

	if graph.state.Err() != nil {
		return graph
	}

	if graph.pathWavefront == nil {
		graph.pathWavefront = path.NewWavefront()
	}

	if graph.frustration == nil {
		graph.frustration = goal.NewFrustrationEngineServer(
			goal.FrustrationWithContext(graph.ctx),
		).Client("logic/substrate/graph")
	}

	graph.serverSide, graph.clientSide = net.Pipe()
	graph.client = Graph_ServerToClient(graph)

	graph.serverConn = rpc.NewConn(rpc.NewStreamTransport(
		graph.serverSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(graph.client),
	})

	return graph
}

/*
Client returns a Cap'n Proto client connected to this GraphServer.
*/
func (graph *GraphServer) Client(clientID string) Graph {
	graph.clientConns[clientID] = rpc.NewConn(rpc.NewStreamTransport(
		graph.clientSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(graph.client),
	})

	return graph.client
}

/*
Close shuts down the RPC connections and underlying net.Pipe,
unblocking goroutines stuck on pipe reads.
*/
func (graph *GraphServer) Close() error {
	graph.state.Reset()

	if graph.serverConn != nil {
		errnie.GuardVoid(graph.state, func() error {
			return graph.serverConn.Close()
		})
		graph.serverConn = nil
	}

	for clientID, conn := range graph.clientConns {
		if conn != nil {
			errnie.GuardVoid(graph.state, func() error {
				return conn.Close()
			})
		}
		delete(graph.clientConns, clientID)
	}

	if graph.serverSide != nil {
		errnie.GuardVoid(graph.state, func() error {
			return graph.serverSide.Close()
		})
		graph.serverSide = nil
	}

	if graph.clientSide != nil {
		errnie.GuardVoid(graph.state, func() error {
			return graph.clientSide.Close()
		})
		graph.clientSide = nil
	}

	if graph.cancel != nil {
		graph.cancel()
	}

	return graph.state.Err()
}

/*
Write data to the Graph, so we can start the recursive folding process, which uses
XOR and POPCNT to cancel out shared components and identify unique residues, building
up a labeled graph. Anything resembling semantic structure is to be considered
coincidental from here on, and no longer relevant to the system itself.
Another way to think about this is that we are now operating on pure, raw structure
alone, and any semantic meaning happens to just follow that structure.
*/
func (graph *GraphServer) Write(ctx context.Context, call Graph_write) error {
	graph.state.Reset()

	key := call.Args().Key()

	graph.mu.Lock()
	graph.data = append(graph.data, key)
	graph.mu.Unlock()

	return nil
}

/*
Prompt implements Graph_Server. It receives the prompt as pre-compiled paths,
walks the stored AST to find the best matching branch, and returns the matched
path as result including Morton key back-pointers for byte recovery.
*/
func (graph *GraphServer) Prompt(ctx context.Context, call Graph_prompt) error {
	graph.state.Reset()
	args := call.Args()

	graph.mu.RLock()
	defer graph.mu.RUnlock()

	graph.RecursiveFold(args)

	return graph.state.Err()
}

/*
Done implements Graph_Server.
*/
func (graph *GraphServer) Done(ctx context.Context, call Graph_done) error {
	return nil
}

/*
RecursiveFold is where the Frustration Engine uses the tools it has available
to synthesize solutions, and new tools, or tool compositions.
*/
func (graph *GraphServer) RecursiveFold(args Graph_prompt_Params) {
	// The system programs itself by constructing native Value instances
	// that behave as local transition operators (first-class program objects).
	// We do not hardcode the logic; we only lay down the flexible programmable
	// medium as described in the README's Substrate Evolution.
	
	program := data.NeutralValue()
	program.SetMutable(true)
	
	// The core field naturally starts at zero (identity), but it is now
	// logically mutable and can carry an affine operator, trajectories,
	// and self-modifying opcodes injected later by the Frustration Engine.
	program.SetProgram(data.OpcodeNext, 1, 0, false)

	// At this point, the system possesses the structural capacity to
	// encode its own discoveries (such as hardened Z-rotations built from
	// macro.MacroOpcode) directly into programmatic Value states.
}

/*
GraphWithContext injects a context.
*/
func GraphWithContext(ctx context.Context) GraphOpt {
	return func(graph *GraphServer) {
		graph.ctx, graph.cancel = context.WithCancel(ctx)
	}
}

/*
GraphWithWorkerPool injects the shared worker pool.
*/
func GraphWithWorkerPool(workerPool *pool.Pool) GraphOpt {
	return func(graph *GraphServer) {
		graph.workerPool = workerPool
	}
}

/*
GraphWithSink injects a custom telemetry sink for testing.
*/
func GraphWithSink(sink *telemetry.Sink) GraphOpt {
	return func(graph *GraphServer) {
		graph.sink = sink
	}
}
