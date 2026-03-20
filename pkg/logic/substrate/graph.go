package substrate

import (
	"context"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/system/cluster"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/telemetry"
	"github.com/theapemachine/six/pkg/validate"
)

var morton = data.NewMortonCoder()

/*
GraphServer implements the Cap'n Proto RPC interface for the logic graph.
It acts as the reasoning engine (Graph), evaluating geometric vector states.
The Machine is the sole orchestrator: it fetches data from SpatialIndex and
hands it to GraphServer via Prompt. GraphServer never calls any other server.
*/
type GraphServer struct {
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	state      *errnie.State
	router     *cluster.Router
	workerPool *pool.Pool
	sink       *telemetry.Sink
	data       [][]primitive.Value
	signals    []primitive.Value

	ptr int
}

/*
GraphOpt configures GraphServer at construction. Options inject context,
worker pool, macro index, or telemetry sink.
*/
type GraphOpt func(*GraphServer)

/*
NewGraphServer creates the Cap'n Proto RPC server for the logic graph and
wires it to a net.Pipe for in-process RPC. Requires ctx and workerPool.
*/
func NewGraphServer(opts ...GraphOpt) *GraphServer {
	graph := &GraphServer{
		state:   errnie.NewState("logic/substrate/graph"),
		sink:    telemetry.NewSink(),
		data:    make([][]primitive.Value, 0),
		signals: make([]primitive.Value, 0),
		ptr:     -1,
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

	return graph
}

/*
Client returns a direct in-process Cap'n Proto client for this server.
No pipes, no goroutines — ServerToClient wires calls straight through.
Satisfies cluster.Service.
*/
func (graph *GraphServer) Client(clientID string) capnp.Client {
	return capnp.Client(Graph_ServerToClient(graph))
}

/*
Close cancels the server context. No pipe cleanup needed because
ServerToClient creates direct in-process clients.
Satisfies cluster.Service.
*/
func (graph *GraphServer) Close() error {
	if graph.cancel != nil {
		graph.cancel()
	}

	return nil
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
	key := call.Args().Key()

	graph.mu.Lock()
	defer graph.mu.Unlock()

	pos, b := morton.Unpack(key)

	if pos == 0 {
		graph.ptr++
	}

	graph.data[graph.ptr] = append(
		graph.data[graph.ptr],
		primitive.SeedObservable(
			b,
			primitive.BaseValue(b).RollLeft(int(pos)),
		),
	)

	return nil
}

/*
Done implements Graph_Server. It is a no-op stub required by the RPC interface;
stream finalization is handled elsewhere (Machine orchestrates tokenizer and graph).
*/
func (graph *GraphServer) Done(ctx context.Context, call Graph_done) error {
	graph.mu.Lock()
	defer graph.mu.Unlock()

	if len(graph.signals) == 0 {
		for _, chunk := range graph.data {
			signal := errnie.Guard(
				graph.state, func() (
					primitive.Value, error,
				) {
					return primitive.New()
				},
			)

			for _, value := range chunk {
				signal = errnie.Guard(
					graph.state, func() (
						primitive.Value, error,
					) {
						return signal.OR(value)
					},
				)
			}

			graph.signals = append(graph.signals, signal)
		}
	}

	graph.RecursiveFold(graph.signals)

	return nil
}

/*
RecursiveFold dynamically folds data into a graph of AST nodes.

EXAMPLE:

	DATA:
		[Sandra] <is in the> [Garden]
		[Roy]    <is in the> [Kitchen]
		[Harold] <is in the> [Kitchen]
			<is in the> the shared component that cancels out, becomes a "label".
			<is in the>   -points to-> [Sandra, Roy, Harold]
			[Sandra]      -points to-> [Garden]
			[Garden]      -points to-> [Sandra]
		    [Roy, Harold] -points to-> [Kitchen]
		    [Kitchen]     -points to-> [Roy, Harold]
	PROMPT:
		Where is Roy?
		Where has no shared component, ignored (if it don't react, it ain't a fact)
		<is> cancels out with <{is} in the> which -points to-> [Sandra, Roy, Harold]
		[Roy] cancels out with [{Roy}] which -points to-> [Kitchen]
	ANSWER:
		<in the> [Kitchen] (left over)
*/
func (graph *GraphServer) RecursiveFold(data []primitive.Value) [][]primitive.Value {
	mid := len(graph.signals) / 2

	left := graph.signals[:mid]
	right := graph.signals[mid:]
	remainderLeft := make([]primitive.Value, 0)
	remainderRight := make([]primitive.Value, 0)

	for _, left := range left {
		label := make([]primitive.Value, 0)
		remainderLeft = append(remainderLeft, left)

		br := false

		for _, right := range right {
			if !br {
				lbl := errnie.Guard(graph.state, func() (primitive.Value, error) {
					return left.AND(right)
				})

				if lbl.CoreActiveCount() == 0 {
					br = true
				}

				label = append(label, lbl)
				continue
			}

			remainderRight = append(remainderRight, right)
		}

		graph.addArrows(label, append(remainderLeft, remainderRight...))
	}

	return graph.RecursiveFold(append(left, right...))
}

func (graph *GraphServer) addArrows(label, remainder []primitive.Value) {
	for idx := range len(remainder) {
		for _, value := range graph.data[idx] {
			remPhase, _ := value.RotationSeed()

			for idx := range label {
				lblPhase, _ := label[idx].RotationSeed()
				label[idx].SetTrajectory(numeric.Phase(lblPhase), numeric.Phase(remPhase))
				label[idx].SetGuardRadius(uint8(label[idx].CoreActiveCount() % 256))
			}
		}
	}
}

/*
emitFoldLabel streams one fold label to the visualizer with its structural
metadata so the front-end can render the hierarchy without an AST tree.
*/
func (graph *GraphServer) emitFoldLabel(
	label primitive.Value,
	level int,
	text string,
	parentBin int,
	childCount int,
) {
	graph.sink.Emit(telemetry.Event{
		Component: "Graph",
		Action:    "Fold",
		Data: telemetry.EventData{
			Bin:        label.Bin(),
			Level:      level,
			Theta:      float64(label.CoreActiveCount()) / 257.0,
			ParentBin:  parentBin,
			ChildCount: childCount,
			Density:    float64(label.CoreActiveCount()) / 257.0,
			ChunkText:  text,
		},
	})
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

/*
GraphWithRouter injects the cluster router so the graph can resolve
Cantilever, HAS, and Reader capabilities at prompt time.
*/
func GraphWithRouter(router *cluster.Router) GraphOpt {
	return func(graph *GraphServer) {
		graph.router = router
	}
}
