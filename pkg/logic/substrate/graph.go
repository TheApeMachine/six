package substrate

import (
	"context"
	"fmt"
	"math"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/path"
	"github.com/theapemachine/six/pkg/numeric/geometry"
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
	ast           *ASTNode
	flatKeys      []uint64
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
		state:       errnie.NewState("logic/substrate/graph"),
		clientConns: map[string]*rpc.Conn{},
		sink:        telemetry.NewSink(),
	}

	for _, opt := range opts {
		opt(graph)
	}

	if graph.pathWavefront == nil {
		graph.pathWavefront = path.NewWavefront()
	}

	errnie.GuardVoid(graph.state, func() error {
		return validate.Require(map[string]any{
			"ctx":           graph.ctx,
			"workerPool":    graph.workerPool,
			"pathWavefront": graph.pathWavefront,
		})
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
	graph.flatKeys = append(graph.flatKeys, key)
	graph.mu.Unlock()

	return nil
}

/*
BuildAST runs RecursiveFold over all ingested sequences and stores the
resulting AST on the GraphServer for subsequent Prompt queries.
*/
func (graph *GraphServer) BuildAST() {
	graph.mu.Lock()
	defer graph.mu.Unlock()

	fmt.Printf("DEBUG: BuildAST called with flatKeys=%d\n", len(graph.flatKeys))

	if len(graph.flatKeys) > 0 {
		cells := data.CompileSequenceCells(graph.flatKeys)

		var currentSeq []data.Value
		var currentMeta []data.Value
		var currentKeys []uint64

		for i, cell := range cells {
			currentSeq = append(currentSeq, data.SeedObservable(cell.Symbol, cell.Value))

			meta := data.MustNewValue()
			meta.CopyFrom(cell.Meta)
			currentMeta = append(currentMeta, meta)

			currentKeys = append(currentKeys, graph.flatKeys[i])
		}

		if len(currentSeq) > 0 {
			graph.sequences = append(graph.sequences, currentSeq)
			graph.metaSequences = append(graph.metaSequences, currentMeta)
			graph.sequenceKeys = append(graph.sequenceKeys, currentKeys)
		}

		graph.flatKeys = nil
	}

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
	graph.state.Reset()
	args := call.Args()

	paths := errnie.Guard(graph.state, func() (capnp.PointerList, error) {
		return args.Paths()
	})

	pathsData := errnie.Guard(graph.state, func() ([][]data.Value, error) {
		return pointerListToValueSlices(paths, graph.state)
	})

	graph.mu.RLock()
	ast := graph.ast
	graph.mu.RUnlock()

	fmt.Printf("DEBUG: Prompt: ast == nil? %v, len(pathsData)=%d\n", ast == nil, len(pathsData))

	if graph.state.Failed() || ast == nil || len(pathsData) == 0 || len(pathsData[0]) == 0 {
		res := errnie.Guard(graph.state, func() (Graph_prompt_Results, error) {
			return call.AllocResults()
		})

		errnie.GuardVoid(graph.state, func() error {
			return graph.writeResult(res, pathsData)
		})

		return graph.state.Err()
	}

	promptUnion := data.ValueLCM(pathsData[0])
	leaf, matchedKeys := ast.Walk(promptUnion)

	resultValues := graph.projectKeysToValues(matchedKeys, leaf, pathsData[0])

	res := errnie.Guard(graph.state, func() (Graph_prompt_Results, error) {
		return call.AllocResults()
	})

	errnie.GuardVoid(graph.state, func() error {
		return graph.writeResult(res, [][]data.Value{resultValues})
	})

	return graph.state.Err()
}

/*
Done implements Graph_Server.
*/
func (graph *GraphServer) Done(ctx context.Context, call Graph_done) error {
	graph.BuildAST()
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
	graph.state.Reset()

	resultList := errnie.Guard(graph.state, func() (capnp.PointerList, error) {
		return res.NewResult(int32(len(paths)))
	})

	seg := res.Segment()

	for i, pathValues := range paths {
		if graph.state.Failed() {
			break
		}

		innerList := errnie.Guard(graph.state, func() (data.Value_List, error) {
			return data.NewValue_List(seg, int32(len(pathValues)))
		})

		if graph.state.Failed() {
			break
		}

		for j, pathValue := range pathValues {
			el := innerList.At(j)
			el.CopyFrom(pathValue)
		}

		errnie.GuardVoid(graph.state, func() error {
			return resultList.Set(i, innerList.ToPtr())
		})
	}

	return graph.state.Err()
}

/*
RecursiveFold fractures data encoded in data.Value types
by using simple bitwise operations (AND, XOR) to identify and isolate shared components.
The shared components then become labels for the remaining fragments.

Example:

	DATA:
		[Sandra]<is in the>[Garden]
		[Roy]<is in the>[Kitchen]
		[Harold]<is in the>[Kitchen]
	FOLDING:
		<is in the> the shared component that cancels out, becomes a "label".
		<is in the>   -points to-> [Sandra, Roy, Harold]
		[Sandra]      -points to-> [Garden]
		[Roy, Harold] -points to-> [Kitchen]
		[Kitchen]     -points to-> [Roy, Harold]
	PROMPT:
		Where is Roy? <is> cancels out with <is in the> which -points to-> [Sandra, Roy, Harold]
		[Roy] cancels out with [Roy] which -points to-> [Kitchen]
		Result: [Kitchen]
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

	errnie.GuardVoid(graph.state, func() error {
		return graph.ctx.Err()
	})

	if graph.state.Failed() {
		return nil
	}

	labelDataValue := extractSharedInvariant(sequences)
	labelMetaValue := extractSharedInvariant(metaSequences)

	fmt.Printf("DEBUG: RecursiveFold level=%d len(sequences)=%d labelDataValue.ActiveCount()=%d labelMetaValue.ActiveCount()=%d\n", level, len(sequences), labelDataValue.ActiveCount(), labelMetaValue.ActiveCount())

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
		errnie.GuardVoid(graph.state, func() error {
			return graph.ctx.Err()
		})

		if graph.state.Failed() {
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

	fmt.Printf("DEBUG: projectKeysToValues: promptLen=%d keysLenInput=%d overlap=%d resultingKeysLen=%d\n", len(promptBytes), len(keys)+overlap, overlap, len(keys))

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
func pointerListToValueSlices(outer capnp.PointerList, state *errnie.State) ([][]data.Value, error) {
	result := make([][]data.Value, outer.Len())

	for i := 0; i < outer.Len(); i++ {
		if state.Failed() {
			break
		}

		ptr := errnie.Guard(state, func() (capnp.Ptr, error) {
			return outer.At(i)
		})

		if state.Failed() {
			break
		}

		inner := data.Value_List(ptr.List())
		row := errnie.Guard(state, func() ([]data.Value, error) {
			return data.ValueListToSlice(inner)
		})

		result[i] = row
	}

	return result, state.Err()
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
	state := errnie.NewState("kernel/substrate/graph/infer-prompt")
	prompt := make([]byte, 0, len(values))

	for index, value := range values {
		errnie.GuardVoid(state, func() error {
			symbol, ok := data.InferLexicalSeed(value)
			if !ok {
				return fmt.Errorf("graph: prompt byte inference failed at %d", index)
			}
			prompt = append(prompt, symbol)
			return nil
		})

		if state.Failed() {
			break
		}
	}

	return prompt, state.Err()
}
