package substrate

import (
	"context"
	"fmt"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
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
	state       *errnie.State
	serverSide  net.Conn
	clientSide  net.Conn
	client      Graph
	serverConn  *rpc.Conn
	clientConns map[string]*rpc.Conn
	workerPool  *pool.Pool
	macroIndex  *macro.MacroIndexServer
	sink        *telemetry.Sink
	data        []uint64
	astRoots    []*ASTNode
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
	graph.clientConns[clientID] = nil

	return Graph_ServerToClient(graph)
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
	_ = ctx
	graph.state.Reset()

	key := call.Args().Key()
	shouldFold := false
	writeSnapshot := []uint64{}

	graph.mu.Lock()
	graph.data = append(graph.data, key)
	writeTokenCount := len(graph.data)

	errnie.GuardVoid(graph.state, func() error {
		batchSize, err := graph.writeFoldBatchSize()
		if err != nil {
			return err
		}

		shouldFold = writeTokenCount >= batchSize && writeTokenCount%batchSize == 0
		if !shouldFold {
			return nil
		}

		writeSnapshot = append(writeSnapshot, graph.data...)

		return nil
	})
	graph.mu.Unlock()

	if graph.state.Failed() {
		return graph.state.Err()
	}

	if !shouldFold {
		return nil
	}

	graph.foldWriteSnapshot(writeSnapshot)

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

	paths := graph.RecursiveFold(args)

	if graph.state.Failed() {
		return graph.state.Err()
	}

	metaPromptSymbols := graph.decodePromptMetaPaths(args)
	resultPaths := graph.resultsFromStoredData(paths, metaPromptSymbols)

	results := errnie.Guard(graph.state, func() (Graph_prompt_Results, error) {
		return call.AllocResults()
	})

	pointerList := errnie.Guard(graph.state, func() (capnp.PointerList, error) {
		return graph.valueMatrixToPointerList(results.Segment(), resultPaths)
	})

	errnie.GuardVoid(graph.state, func() error {
		return results.SetResult(pointerList)
	})

	return graph.state.Err()
}

/*
Done implements Graph_Server. It is a no-op stub required by the RPC interface;
stream finalization is handled elsewhere (Machine orchestrates tokenizer and graph).
*/
func (graph *GraphServer) Done(ctx context.Context, call Graph_done) error {
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
func (graph *GraphServer) RecursiveFold(args Graph_prompt_Params) [][]primitive.Value {
	paths := errnie.Guard(graph.state, func() ([][]primitive.Value, error) {
		return graph.decodePromptPaths(args)
	})

	if graph.state.Failed() {
		return nil
	}

	graph.foldDecodedPaths(paths)
	graph.recordDerivedTools(paths)

	return paths
}

/*
foldWriteSnapshot projects ingested Morton keys into one value path and folds it.
*/
func (graph *GraphServer) foldWriteSnapshot(keys []uint64) {
	if len(keys) == 0 {
		return
	}

	paths := [][]primitive.Value{
		primitive.CompileObservableSequenceValues(keys),
	}

	graph.foldDecodedPaths(paths)
}

/*
foldDecodedPaths rebuilds AST roots from already decoded value paths.
*/
func (graph *GraphServer) foldDecodedPaths(paths [][]primitive.Value) {
	graph.mu.Lock()
	defer graph.mu.Unlock()

	graph.astRoots = graph.astRoots[:0]

	for _, buffer := range paths {
		if len(buffer) == 0 {
			continue
		}

		symbols := graph.decodePromptSymbols(buffer)

		root := graph.recursiveFoldSpan(buffer, 0, len(buffer), 0, symbols)
		if root != nil {
			graph.astRoots = append(graph.astRoots, root)
			graph.emitFoldTelemetry(root, -1)
		}
	}

	graph.sink.Emit(telemetry.Event{
		Component: "Graph",
		Action:    "Evaluate",
		Data: telemetry.EventData{
			PathCount: len(paths),
		},
	})

	console.Trace("recursiveFold", "astRoots", graph.astRoots)
}

/*
emitFoldTelemetry streams fold node topology to the visualizer.
*/
func (graph *GraphServer) emitFoldTelemetry(node *ASTNode, parentBin int) {
	if node == nil {
		return
	}

	graph.sink.Emit(telemetry.Event{
		Component: "Graph",
		Action:    "Fold",
		Data: telemetry.EventData{
			Bin:        node.Bin,
			Level:      node.Level,
			Theta:      node.Theta,
			ParentBin:  parentBin,
			ChildCount: len(node.Children),
			Density:    float64(node.Label.CoreActiveCount()) / 257.0,
			ChunkText:  node.Text,
		},
	})

	for _, child := range node.Children {
		graph.emitFoldTelemetry(child, node.Bin)
	}
}

/*
writeFoldBatchSize derives the ingest batch size that triggers RecursiveFold.
*/
func (graph *GraphServer) writeFoldBatchSize() (int, error) {
	metrics := graph.workerPool.Metrics()
	if metrics == nil {
		return 0, fmt.Errorf("graph write fold batch: missing worker metrics")
	}

	snapshot := metrics.ExportMetrics()
	workerCountRaw, ok := snapshot["worker_count"]
	if !ok {
		return 0, fmt.Errorf("graph write fold batch: worker_count missing")
	}

	workerCount, ok := workerCountRaw.(int)
	if !ok {
		return 0, fmt.Errorf("graph write fold batch: worker_count type %T", workerCountRaw)
	}

	if workerCount <= 0 {
		return 0, fmt.Errorf("graph write fold batch: invalid worker_count %d", workerCount)
	}

	return workerCount * 8, nil
}

/*
decodePromptPaths converts Graph prompt rows into Value buffers.
*/
func (graph *GraphServer) decodePromptPaths(
	args Graph_prompt_Params,
) ([][]primitive.Value, error) {
	pathMatrix := errnie.Guard(graph.state, func() (capnp.PointerList, error) {
		return args.Paths()
	})

	console.Trace("decodePromptPaths", "pathMatrix", pathMatrix)

	decoded := make([][]primitive.Value, 0, pathMatrix.Len())

	for row := 0; row < pathMatrix.Len(); row++ {
		ptr, err := pathMatrix.At(row)
		if err != nil {
			return nil, err
		}

		values, err := primitive.ValueListToSlice(primitive.Value_List(ptr.List()))
		if err != nil {
			return nil, err
		}

		decoded = append(decoded, values)
	}

	return decoded, nil
}

/*
recordDerivedTools closes the fold-to-tool loop by deriving one boundary opcode
per path and feeding the execution residue back into MacroIndex candidates.
*/
func (graph *GraphServer) recordDerivedTools(paths [][]primitive.Value) {
	if graph.macroIndex == nil {
		return
	}

	for _, path := range paths {
		if len(path) < 2 {
			continue
		}

		start := path[0]
		end := path[len(path)-1]
		key := macro.AffineKeyFromValues(start, end)

		preDelta := errnie.Guard(graph.state, func() (primitive.Value, error) {
			return start.XOR(end)
		})
		if graph.state.Failed() {
			return
		}

		preResidue := preDelta.CoreActiveCount()
		graph.macroIndex.RecordOpcode(key)

		opcode, found := graph.macroIndex.FindOpcode(key)
		if !found || opcode == nil {
			continue
		}

		recovered := start.ApplyAffineValue(opcode.Scale, opcode.Translate)

		postDelta := errnie.Guard(graph.state, func() (primitive.Value, error) {
			return recovered.XOR(end)
		})
		if graph.state.Failed() {
			return
		}

		postResidue := postDelta.CoreActiveCount()
		advanced := postResidue < preResidue
		stable := postResidue == 0

		graph.macroIndex.RecordCandidateResult(
			key,
			preResidue,
			postResidue,
			advanced,
			stable,
		)
	}
}

/*
valueMatrixToPointerList serializes Value paths into a Graph prompt/result matrix.
*/
func (graph *GraphServer) valueMatrixToPointerList(
	segment *capnp.Segment,
	paths [][]primitive.Value,
) (capnp.PointerList, error) {
	_ = graph

	list, err := capnp.NewPointerList(segment, int32(len(paths)))
	if err != nil {
		return capnp.PointerList{}, err
	}

	for rowIndex, row := range paths {
		valueList, err := primitive.NewValue_List(segment, int32(len(row)))
		if err != nil {
			return capnp.PointerList{}, err
		}

		for colIndex, value := range row {
			dst := valueList.At(colIndex)
			dst.CopyFrom(value)
		}

		if err := list.Set(rowIndex, valueList.ToPtr()); err != nil {
			return capnp.PointerList{}, err
		}
	}

	return list, nil
}

/*
resultsFromStoredData resolves prompt prefixes against stored dataset sequences.
When an exact prefix match is found, the full matched sequence is returned so the
machine can decode only the continuation after skipping prompt length.
*/
func (graph *GraphServer) resultsFromStoredData(
	paths [][]primitive.Value,
	metaPromptSymbols [][]byte,
) [][]primitive.Value {
	graph.mu.RLock()
	keys := append([]uint64(nil), graph.data...)
	graph.mu.RUnlock()

	sequences := graph.splitStoredSequences(keys)
	if len(sequences) == 0 {
		return paths
	}

	results := make([][]primitive.Value, 0, len(paths))

	for index, path := range paths {
		promptSymbols := graph.decodePromptSymbols(path)
		if index < len(metaPromptSymbols) && len(metaPromptSymbols[index]) > 0 {
			promptSymbols = metaPromptSymbols[index]
		}

		if len(promptSymbols) == 0 {
			results = append(results, path)
			continue
		}

		matched := graph.findMatchingSequence(sequences, promptSymbols)
		if len(matched) == 0 {
			results = append(results, path)
			continue
		}

		results = append(results, graph.sequenceToValues(matched))
	}

	return results
}

/*
decodePromptMetaPaths decodes lexical prompt symbols from Graph meta paths.
*/
func (graph *GraphServer) decodePromptMetaPaths(args Graph_prompt_Params) [][]byte {
	metaMatrix, err := args.MetaPaths()
	if err != nil || metaMatrix.Len() == 0 {
		return nil
	}

	out := make([][]byte, 0, metaMatrix.Len())

	for row := 0; row < metaMatrix.Len(); row++ {
		ptr, ptrErr := metaMatrix.At(row)
		if ptrErr != nil {
			out = append(out, nil)
			continue
		}

		values, valueErr := primitive.ValueListToSlice(primitive.Value_List(ptr.List()))
		if valueErr != nil {
			out = append(out, nil)
			continue
		}

		symbols := make([]byte, 0, len(values))
		for _, value := range values {
			symbol, ok := primitive.InferLexicalSeed(value)
			if ok {
				symbols = append(symbols, symbol)
			}
		}

		out = append(out, symbols)
	}

	return out
}

/*
splitStoredSequences separates one key stream into contiguous boundary-local spans.
*/
func (graph *GraphServer) splitStoredSequences(keys []uint64) [][]uint64 {
	_ = graph

	if len(keys) == 0 {
		return nil
	}

	return [][]uint64{keys}
}

/*
decodePromptSymbols projects one prompt path back into lexical symbols.
*/
func (graph *GraphServer) decodePromptSymbols(path []primitive.Value) []byte {
	_ = graph

	symbols := make([]byte, 0, len(path))

	for _, value := range path {
		symbol, ok := primitive.InferLexicalSeed(primitive.Value(value))
		if !ok {
			continue
		}

		symbols = append(symbols, symbol)
	}

	return symbols
}

/*
findMatchingSequence locates the first stored sequence window matching the prompt.
It returns the matched suffix starting at the prompt anchor.
*/
func (graph *GraphServer) findMatchingSequence(
	sequences [][]uint64,
	promptSymbols []byte,
) []uint64 {
	_ = graph

	coder := data.NewMortonCoder()
	bestMatch := []uint64{}

	for _, sequence := range sequences {
		if len(sequence) < len(promptSymbols) {
			continue
		}

		for startIndex := 0; startIndex <= len(sequence)-len(promptSymbols); startIndex++ {
			matched := true

			for index, expected := range promptSymbols {
				_, symbol := coder.Unpack(sequence[startIndex+index])
				if symbol != expected {
					matched = false
					break
				}
			}

			if matched {
				candidate := graph.trimMatchedSequence(sequence[startIndex:], len(promptSymbols))
				if len(candidate) > len(bestMatch) {
					bestMatch = candidate
				}
			}
		}
	}

	return bestMatch
}

/*
trimMatchedSequence keeps one matched stream until its first structural boundary.
*/
func (graph *GraphServer) trimMatchedSequence(sequence []uint64, promptLen int) []uint64 {
	_ = graph

	if len(sequence) <= promptLen {
		return sequence
	}

	coder := data.NewMortonCoder()

	for index := promptLen; index < len(sequence); index++ {
		previousPosition, _ := coder.Unpack(sequence[index-1])
		currentPosition, _ := coder.Unpack(sequence[index])
		if currentPosition <= previousPosition {
			return sequence[:index]
		}
	}

	return sequence
}

/*
sequenceToValues compiles one stored key sequence into observable primitive Values.
*/
func (graph *GraphServer) sequenceToValues(sequence []uint64) []primitive.Value {
	_ = graph

	cells := primitive.CompileSequenceCells(sequence)
	values := make([]primitive.Value, 0, len(cells))

	for _, cell := range cells {
		values = append(values, primitive.SeedObservable(cell.Symbol, cell.Value))
	}

	return values
}

/*
recursiveFoldSpan partitions a buffer into powers-of-two halves, extracts shared
structure as a label, and links children in an in-memory AST.
*/
func (graph *GraphServer) recursiveFoldSpan(
	buffer []primitive.Value,
	start int,
	end int,
	level int,
	symbols []byte,
) *ASTNode {
	_ = graph

	if start >= end {
		return nil
	}

	spanText := ""
	if len(symbols) >= end {
		spanText = string(symbols[start:end])
	} else if len(symbols) > start {
		spanText = string(symbols[start:])
	}

	if end-start == 1 {
		leaf := &ASTNode{
			Level:  level,
			Label:  buffer[start],
			Bin:    buffer[start].Bin(),
			Text:   spanText,
			Leaves: [][]primitive.Value{{buffer[start]}},
		}

		return leaf
	}

	mid := start + ((end - start) / 2)

	graph.sink.Emit(telemetry.Event{
		Component: "Graph",
		Action:    "FoldSpan",
		Data: telemetry.EventData{
			Level:     level,
			Left:      start,
			Right:     end,
			SpanSize:  end - start,
			ChunkText: spanText,
		},
	})

	left := graph.recursiveFoldSpan(buffer, start, mid, level+1, symbols)
	right := graph.recursiveFoldSpan(buffer, mid, end, level+1, symbols)

	leftAggregate := graph.aggregateSpan(buffer, start, mid)
	rightAggregate := graph.aggregateSpan(buffer, mid, end)
	label := errnie.Guard(graph.state, func() (primitive.Value, error) {
		return leftAggregate.AND(rightAggregate)
	})

	node := &ASTNode{
		Level:     level,
		Label:     label,
		LabelMeta: graph.arrowMeta(leftAggregate, rightAggregate, label, level),
		Bin:       label.Bin(),
		Theta:     float64(label.CoreActiveCount()) / 257.0,
		Text:      spanText,
		Children:  []*ASTNode{},
	}

	if left != nil {
		node.Children = append(node.Children, left)
	}

	if right != nil {
		node.Children = append(node.Children, right)
	}

	return node
}

/*
aggregateSpan computes an OR union over [start:end) without mutating source data.
*/
func (graph *GraphServer) aggregateSpan(
	buffer []primitive.Value,
	start int,
	end int,
) primitive.Value {
	_ = graph

	initialized := false
	var aggregate primitive.Value

	for index := start; index < end; index++ {
		if buffer[index].ActiveCount() == 0 {
			continue
		}

		if !initialized {
			aggregate = buffer[index]
			initialized = true
			continue
		}

		aggregate = errnie.Guard(graph.state, func() (primitive.Value, error) {
			return aggregate.OR(buffer[index])
		})
	}

	if !initialized {
		return primitive.Value{}
	}

	return aggregate
}

/*
arrowMeta stores directional fold hints ("arrow of time") in shell fields.
*/
func (graph *GraphServer) arrowMeta(
	left primitive.Value,
	right primitive.Value,
	label primitive.Value,
	level int,
) primitive.Value {
	_ = graph

	meta := primitive.NeutralValue()

	fromPhase, _ := left.RotationSeed()
	toPhase, _ := right.RotationSeed()
	meta.SetTrajectory(numeric.Phase(fromPhase), numeric.Phase(toPhase))

	meta.SetRouteHint(uint8(label.Bin()))
	meta.SetGuardRadius(uint8(label.CoreActiveCount() % 256))
	meta.SetMutable(level > 0)

	return meta
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
GraphWithMacroIndex injects the shared macro index for autonomous tool reuse.
*/
func GraphWithMacroIndex(index *macro.MacroIndexServer) GraphOpt {
	return func(graph *GraphServer) {
		graph.macroIndex = index
	}
}

/*
MacroIndex returns the shared macro index currently wired into GraphServer.
*/
func (graph *GraphServer) MacroIndex() *macro.MacroIndexServer {
	return graph.macroIndex
}

/*
GraphWithSink injects a custom telemetry sink for testing.
*/
func GraphWithSink(sink *telemetry.Sink) GraphOpt {
	return func(graph *GraphServer) {
		graph.sink = sink
	}
}
