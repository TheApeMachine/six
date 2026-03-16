package substrate

import (
	"context"
	"fmt"
	"math"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/numeric/geometry"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/system/console"
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
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	serverSide  net.Conn
	clientSide  net.Conn
	client      Graph
	serverConn  *rpc.Conn
	clientConns map[string]*rpc.Conn
	workerPool  *pool.Pool
	sink        *telemetry.Sink
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
		clientConns: map[string]*rpc.Conn{},
		sink:        telemetry.NewSink(),
	}

	for _, opt := range opts {
		opt(graph)
	}

	validate.Require(map[string]any{
		"ctx":        graph.ctx,
		"workerPool": graph.workerPool,
	})

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
	if graph.serverConn != nil {
		_ = graph.serverConn.Close()
		graph.serverConn = nil
	}

	for clientID, conn := range graph.clientConns {
		if conn != nil {
			_ = conn.Close()
		}
		delete(graph.clientConns, clientID)
	}

	if graph.serverSide != nil {
		_ = graph.serverSide.Close()
		graph.serverSide = nil
	}
	if graph.clientSide != nil {
		_ = graph.clientSide.Close()
		graph.clientSide = nil
	}
	if graph.cancel != nil {
		graph.cancel()
	}

	return nil
}

/*
Prompt implements Graph_Server. It receives pre-fetched paths from the
Machine, applies RecursiveFold reasoning, and returns the result paths.
*/
func (graph *GraphServer) Prompt(ctx context.Context, call Graph_prompt) error {
	args := call.Args()

	paths, err := args.Paths()
	if err != nil {
		return console.Error(err)
	}

	metaPaths, err := args.MetaPaths()
	if err != nil {
		return console.Error(err)
	}

	pathsData, err := pointerListToValueSlices(paths)
	if err != nil {
		return console.Error(err)
	}

	metaPathsData, err := pointerListToValueSlices(metaPaths)
	if err != nil {
		return console.Error(err)
	}

	graph.RecursiveFold(pathsData, metaPathsData, 0, -1)

	res, err := call.AllocResults()
	if err != nil {
		return console.Error(err)
	}

	return graph.writeResult(res, pathsData)
}

/*
Done implements Graph_Server.
*/
func (graph *GraphServer) Done(ctx context.Context, call Graph_done) error {
	return nil
}

/*
Evaluate sweeps a prompt value against a contiguous path matrix via XOR + POPCNT.
*/
func (graph *GraphServer) Evaluate(
	prompt data.Value, paths []data.Value,
	interest *data.Value, danger *data.Value,
) (bestIdx int, lowestEnergy int, residue data.Value) {
	lowestEnergy = math.MaxInt32
	bestIdx = -1

	for i, path := range paths {
		res := prompt.XOR(path)
		energy := res.ActiveCount()

		if interest != nil {
			resonance := path.AND(*interest)
			energy -= resonance.ActiveCount()
		}

		if danger != nil {
			punish := path.AND(*danger)
			energy += punish.ActiveCount()
		}

		if energy < lowestEnergy {
			lowestEnergy = energy
			bestIdx = i
			residue = res
		}
	}

	return bestIdx, lowestEnergy, residue
}

func (graph *GraphServer) writeResult(res Graph_prompt_Results, paths [][]data.Value) error {
	resultList, err := res.NewResult(int32(len(paths)))
	if err != nil {
		return console.Error(err)
	}

	seg := res.Segment()

	for i, pathValues := range paths {
		innerList, err := data.NewValue_List(seg, int32(len(pathValues)))
		if err != nil {
			return console.Error(err)
		}

		for j, pathValue := range pathValues {
			el := innerList.At(j)
			el.CopyFrom(pathValue)
		}

		if err := resultList.Set(i, innerList.ToPtr()); err != nil {
			return console.Error(err)
		}
	}

	return nil
}

/*
RecursiveFold fractures geometric sequences into an isolated hierarchy of
labels connected by phase rotations, firing pool jobs recursively.
*/
func (graph *GraphServer) RecursiveFold(
	sequences [][]data.Value,
	metaSequences [][]data.Value,
	level int,
	parentBin int,
) {
	if len(sequences) == 0 || len(metaSequences) == 0 {
		return
	}

	if graph.ctx.Err() != nil {
		return
	}

	labelDataValue := extractSharedInvariant(sequences)
	labelMetaValue := extractSharedInvariant(metaSequences)

	if labelDataValue.ActiveCount() == 0 {
		return
	}

	labelBin := data.ValueBin(&labelDataValue)

	ei := geometry.NewEigenMode()
	theta, _ := ei.PhaseForValue(&labelMetaValue)

	var uniqueResidues [][]data.Value
	var uniqueMetaResidues [][]data.Value

	for i, seq := range sequences {
		metaSeq := metaSequences[i]
		residue := xorSequence(seq, labelDataValue)
		metaResidue := xorSequence(metaSeq, labelMetaValue)

		if len(residue) > 0 {
			uniqueResidues = append(uniqueResidues, residue)
			uniqueMetaResidues = append(uniqueMetaResidues, metaResidue)
		}
	}

	graph.sink.Emit(telemetry.Event{
		Component: "Graph",
		Action:    "Fold",
		Data: telemetry.EventData{
			Bin:        labelBin,
			Level:      level,
			ParentBin:  parentBin,
			ChildCount: len(uniqueResidues),
			ActiveBits: data.ValuePrimeIndices(&labelMetaValue),
			Density:    labelMetaValue.ShannonDensity(),
			Theta:      theta,
		},
	})

	for index, resSeq := range uniqueResidues {
		if graph.ctx.Err() != nil {
			return
		}

		metaResSeq := uniqueMetaResidues[index]
		jobID := fmt.Sprintf("fold-level-%d-seq-%d", level, index)

		graph.workerPool.Schedule(jobID, func(ctx context.Context) (any, error) {
			graph.RecursiveFold(
				[][]data.Value{resSeq},
				[][]data.Value{metaResSeq},
				level+1,
				labelBin,
			)
			return nil, nil
		})
	}
}

/*
pointerListToValueSlices converts a capnp.PointerList (List(List(Value))) to [][]data.Value.
*/
func pointerListToValueSlices(outer capnp.PointerList) ([][]data.Value, error) {
	result := make([][]data.Value, outer.Len())

	for i := 0; i < outer.Len(); i++ {
		ptr, err := outer.At(i)
		if err != nil {
			return nil, err
		}

		inner := data.Value_List(ptr.List())
		row, err := data.ValueListToSlice(inner)
		if err != nil {
			return nil, err
		}

		result[i] = row
	}

	return result, nil
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
