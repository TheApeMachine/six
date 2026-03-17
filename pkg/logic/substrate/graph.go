package substrate

import (
	"context"
	"fmt"
	"math"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/logic/path"
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
	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	serverSide    net.Conn
	clientSide    net.Conn
	client        Graph
	serverConn    *rpc.Conn
	clientConns   map[string]*rpc.Conn
	workerPool    *pool.Pool
	sink          *telemetry.Sink
	pathWavefront *path.Wavefront
	ast           *ASTNode
	sequences     [][]data.Value
	metaSequences [][]data.Value
	sequenceKeys  [][]uint64
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

	if graph.pathWavefront == nil {
		graph.pathWavefront = path.NewWavefront()
	}

	validate.Require(map[string]any{
		"ctx":           graph.ctx,
		"workerPool":    graph.workerPool,
		"pathWavefront": graph.pathWavefront,
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
Write data to the Graph, so we can start the recursive folding process, which uses
XOR and POPCNT to cancel out shared components and identify unique residues, building
up a labeled graph. Anything resembling semantic structure is to be considered
coincidental from here on, and no longer relevant to the system itself.
Another way to think about this is that we are now operating on pure, raw structure
alone, and any semantic meaning happens to just follow that structure.
*/
func (graph *GraphServer) Write(ctx context.Context, call Graph_write) error {
	_ = call.Args()
	return nil
}

/*
BuildAST runs RecursiveFold over all ingested sequences and stores the
resulting AST on the GraphServer for subsequent Prompt queries.
*/
func (graph *GraphServer) BuildAST() {
	graph.mu.Lock()
	defer graph.mu.Unlock()

	if len(graph.sequences) == 0 {
		return
	}

	graph.ast = graph.RecursiveFold(
		graph.sequences,
		graph.metaSequences,
		graph.sequenceKeys,
		0, -1,
	)
}

/*
Prompt implements Graph_Server. It receives the prompt as pre-compiled paths,
walks the stored AST to find the best matching branch, and returns the matched
path as result including Morton key back-pointers for byte recovery.
*/
func (graph *GraphServer) Prompt(ctx context.Context, call Graph_prompt) error {
	args := call.Args()

	paths, err := args.Paths()
	if err != nil {
		return console.Error(err)
	}

	pathsData, err := pointerListToValueSlices(paths)
	if err != nil {
		return console.Error(err)
	}

	graph.mu.RLock()
	ast := graph.ast
	graph.mu.RUnlock()

	if ast == nil || len(pathsData) == 0 || len(pathsData[0]) == 0 {
		res, err := call.AllocResults()
		if err != nil {
			return console.Error(err)
		}

		return graph.writeResult(res, pathsData)
	}

	promptUnion := data.ValueLCM(pathsData[0])
	leaf, matchedKeys := ast.Walk(promptUnion)

	resultValues := graph.projectKeysToValues(matchedKeys, leaf, pathsData[0])

	res, err := call.AllocResults()
	if err != nil {
		return console.Error(err)
	}

	return graph.writeResult(res, [][]data.Value{resultValues})
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

func (graph *GraphServer) stabilizePaths(
	pathsData [][]data.Value,
	metaPathsData [][]data.Value,
) ([][]data.Value, [][]data.Value, error) {
	if graph.pathWavefront == nil || !graph.pathWavefront.CanStabilize(pathsData) {
		return pathsData, metaPathsData, nil
	}

	return graph.pathWavefront.Stabilize(pathsData, metaPathsData)
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
RecursiveFold fractures data encoded in data.Value types
by using simple bitwise operations (AND, XOR) to identify and isolate shared components.
Returns an ASTNode tree where each node carries the shared invariant (Label)
and Morton key back-pointers for byte recovery via the Tree.
*/
func (graph *GraphServer) RecursiveFold(
	sequences [][]data.Value,
	metaSequences [][]data.Value,
	keys [][]uint64,
	level int,
	parentBin int,
) *ASTNode {
	if len(sequences) == 0 || len(metaSequences) == 0 {
		return nil
	}

	if graph.ctx.Err() != nil {
		return nil
	}

	labelDataValue := extractSharedInvariant(sequences)
	labelMetaValue := extractSharedInvariant(metaSequences)

	if labelDataValue.ActiveCount() == 0 {
		return nil
	}

	labelBin := labelDataValue.Bin()

	ei := geometry.NewEigenMode()
	theta, _ := ei.PhaseForValue(&labelMetaValue)

	allKeys := flattenKeys(keys)

	node := &ASTNode{
		Level:     level,
		Bin:       labelBin,
		Label:     labelDataValue,
		LabelMeta: labelMetaValue,
		Theta:     theta,
		Keys:      allKeys,
	}

	var uniqueResidues [][]data.Value
	var uniqueMetaResidues [][]data.Value
	var residueKeys [][]uint64

	for i, seq := range sequences {
		metaSeq := metaSequences[i]
		residue := xorSequence(seq, labelDataValue)
		metaResidue := xorSequence(metaSeq, labelMetaValue)

		if len(residue) > 0 {
			uniqueResidues = append(uniqueResidues, residue)
			uniqueMetaResidues = append(uniqueMetaResidues, metaResidue)

			if i < len(keys) {
				residueKeys = append(residueKeys, keys[i])
			}
		} else if len(seq) > 0 {
			node.Leaves = append(node.Leaves, seq)
		}
	}

	if len(uniqueResidues) == 0 || (len(sequences) == 1 && len(uniqueResidues) == 1) {
		node.Leaves = append(node.Leaves, uniqueResidues...)
		return node
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
			break
		}

		metaResSeq := uniqueMetaResidues[index]

		var childKeys [][]uint64
		if index < len(residueKeys) {
			childKeys = [][]uint64{residueKeys[index]}
		}

		child := graph.RecursiveFold(
			[][]data.Value{resSeq},
			[][]data.Value{metaResSeq},
			childKeys,
			level+1,
			labelBin,
		)

		if child != nil {
			node.Children = append(node.Children, child)
		}
	}

	return node
}

/*
projectKeysToValues takes the Morton keys from the AST leaf and builds
the result as [promptValues... | answer values...]. The leaf keys represent
the full matched sentence; we skip the prefix that overlaps with the prompt
so only the continuation (the answer) follows the prompt in the result.
*/
func (graph *GraphServer) projectKeysToValues(
	keys []uint64,
	leaf *ASTNode,
	promptValues []data.Value,
) []data.Value {
	coder := data.NewMortonCoder()

	promptBytes := make([]byte, 0, len(promptValues))
	for _, value := range promptValues {
		symbol, ok := data.InferLexicalSeed(value)
		if ok {
			promptBytes = append(promptBytes, symbol)
		}
	}

	overlap := 0
	for i, key := range keys {
		if overlap >= len(promptBytes) {
			overlap = i
			break
		}

		_, symbol := coder.Unpack(key)
		if overlap < len(promptBytes) && symbol == promptBytes[overlap] {
			overlap++
		}
	}

	if overlap >= len(promptBytes) && overlap < len(keys) {
		keys = keys[overlap:]
	} else if overlap >= len(keys) {
		keys = nil
	}

	result := make([]data.Value, 0, len(promptValues)+len(keys))

	for _, value := range promptValues {
		result = append(result, value)
	}

	for _, key := range keys {
		_, symbol := coder.Unpack(key)
		result = append(result, data.BaseValue(symbol))
	}

	return result
}

func flattenKeys(keys [][]uint64) []uint64 {
	var flat []uint64

	for _, batch := range keys {
		flat = append(flat, batch...)
	}

	return flat
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

/*
GraphWithPathWavefront injects a graph-local path stabilizer.
*/
func GraphWithPathWavefront(pathWavefront *path.Wavefront) GraphOpt {
	return func(graph *GraphServer) {
		graph.pathWavefront = pathWavefront
	}
}

func inferPromptBytes(values []data.Value) ([]byte, error) {
	prompt := make([]byte, 0, len(values))

	for index, value := range values {
		symbol, ok := data.InferLexicalSeed(value)
		if !ok {
			return nil, fmt.Errorf(
				"graph: prompt byte inference failed at %d",
				index,
			)
		}

		prompt = append(prompt, symbol)
	}

	return prompt, nil
}
