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
Load reports how many prompt rows are currently held for folding.
*/
func (graph *GraphServer) Load() int64 {
	graph.mu.RLock()
	defer graph.mu.RUnlock()

	n := graph.ptr + 1
	if n < 0 {
		return 0
	}

	return int64(n)
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

	for len(graph.data) <= graph.ptr {
		graph.data = append(graph.data, nil)
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
	if len(data) <= 1 {
		if len(data) == 0 {
			return nil
		}

		return [][]primitive.Value{append([]primitive.Value(nil), data...)}
	}

	mid := len(data) / 2

	leftSlice := data[:mid]
	rightSlice := data[mid:]

	remainderLeft := make([]primitive.Value, 0)
	remainderRight := make([]primitive.Value, 0)

	for _, leftItem := range leftSlice {
		label := make([]primitive.Value, 0)
		remainderLeft = append(remainderLeft, leftItem)

		matched := false

		for _, rightItem := range rightSlice {
			if !matched {
				lbl := errnie.Guard(graph.state, func() (primitive.Value, error) {
					return leftItem.AND(rightItem)
				})

				if graph.state.Failed() {
					return [][]primitive.Value{append([]primitive.Value(nil), data...)}
				}

				if lbl.CoreActiveCount() == 0 {
					matched = true
				}

				label = append(label, lbl)

				continue
			}

			remainderRight = append(remainderRight, rightItem)
		}

		graph.addArrows(label, append(remainderLeft, remainderRight...))
	}

	return [][]primitive.Value{append([]primitive.Value(nil), data...)}
}

/*
ExactContinuation returns the exact remainder for the first row whose prefix
matches the prompt Values byte-for-byte across the full Value shell.
*/
func (graph *GraphServer) ExactContinuation(
	prompt []primitive.Value,
) []primitive.Value {
	graph.mu.RLock()
	defer graph.mu.RUnlock()

	for _, row := range graph.data {
		if len(row) <= len(prompt) {
			continue
		}

		if !graph.hasExactPrefix(row, prompt) {
			continue
		}

		return append([]primitive.Value(nil), row[len(prompt):]...)
	}

	return nil
}

/*
hasExactPrefix checks whether prompt matches the leading Values in row exactly.
*/
func (graph *GraphServer) hasExactPrefix(
	row []primitive.Value,
	prompt []primitive.Value,
) bool {
	if len(prompt) == 0 || len(row) < len(prompt) {
		return false
	}

	for index := range prompt {
		if !graph.valuesEqual(row[index], prompt[index]) {
			return false
		}
	}

	return true
}

/*
valuesEqual compares the full 8-word Value shell with no fuzzy matching.
*/
func (graph *GraphServer) valuesEqual(
	left primitive.Value,
	right primitive.Value,
) bool {
	return left.C0() == right.C0() &&
		left.C1() == right.C1() &&
		left.C2() == right.C2() &&
		left.C3() == right.C3() &&
		left.C4() == right.C4() &&
		left.C5() == right.C5() &&
		left.C6() == right.C6() &&
		left.C7() == right.C7()
}

func (graph *GraphServer) addArrows(label, remainder []primitive.Value) {
	for rIdx := range remainder {
		if rIdx >= len(graph.data) {
			continue
		}

		for _, value := range graph.data[rIdx] {
			remPhase, _ := value.RotationSeed()

			for lIdx := range label {
				lblPhase, _ := label[lIdx].RotationSeed()
				label[lIdx].SetTrajectory(numeric.Phase(lblPhase), numeric.Phase(remPhase))
				label[lIdx].SetGuardRadius(uint8(label[lIdx].CoreActiveCount() % 256))
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
