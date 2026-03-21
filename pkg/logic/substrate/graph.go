package substrate

import (
	"context"
	"sort"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/logic/topology"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/system/cluster"
	config "github.com/theapemachine/six/pkg/system/core"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/telemetry"
	"github.com/theapemachine/six/pkg/validate"
)

var morton = data.NewMortonCoder()

/*
FoldNode is a persistent fold product from the RecursiveFold hierarchy.
Each node records the shared structural invariant (Label) extracted when
two values merged, plus the directional residues left after subtracting
that invariant. The hierarchy forms the reasoning graph that prompt-time
queries traverse.
*/
type FoldNode struct {
	Label     primitive.Value
	ResidueA  primitive.Value
	ResidueB  primitive.Value
	SourceA   int
	SourceB   int
	Threshold float64
	Depth     int
}

/*
similarityPair holds a precomputed Jaccard similarity between two values.
Sorted by decreasing similarity to determine topology-guided merge order.
*/
type similarityPair struct {
	indexA     int
	indexB     int
	similarity float64
}

/*
GraphServer implements the Cap'n Proto RPC interface for the logic graph.
It acts as the reasoning engine (Graph), evaluating geometric vector states.
The Machine is the sole orchestrator: it fetches data from SpatialIndex and
hands it to GraphServer via Prompt. GraphServer never calls any other server.
*/
type GraphServer struct {
	mu           sync.RWMutex
	clientMu     sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	state        *errnie.State
	router       *cluster.Router
	workerPool   *pool.Pool
	sink         *telemetry.Sink
	data         [][]primitive.Value
	signals      []primitive.Value
	foldGraph    []FoldNode
	cachedClient capnp.Client

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
		state:     errnie.NewState("logic/substrate/graph"),
		sink:      telemetry.NewSink(),
		data:      make([][]primitive.Value, 0),
		signals:   make([]primitive.Value, 0),
		foldGraph: make([]FoldNode, 0, 64),
		ptr:       -1,
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
Client returns a cached in-process Cap'n Proto client for this server.
ServerToClient spawns a handleCalls goroutine per call, so we create
the client once and reuse it to avoid goroutine leaks.
*/
func (graph *GraphServer) Client(clientID string) capnp.Client {
	graph.clientMu.Lock()
	defer graph.clientMu.Unlock()

	if !graph.cachedClient.IsValid() {
		graph.cachedClient = capnp.Client(Graph_ServerToClient(graph))
	}

	return graph.cachedClient
}

/*
Load reports how many prompt rows are currently held for folding.
*/
func (graph *GraphServer) Load() int64 {
	graph.mu.RLock()
	defer graph.mu.RUnlock()

	return int64(graph.ptr + 1)
}

/*
Close releases the cached client and cancels the server context.
*/
func (graph *GraphServer) Close() error {
	graph.clientMu.Lock()
	if graph.cachedClient.IsValid() {
		graph.cachedClient.Release()
	}
	graph.clientMu.Unlock()

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
If that sounds scary to you, consider that you can only assume that my assumption
that human language semantics are less important than the structure of the language
itself, is incorrect. And you may well be right, the difference is that I understand
the need to prove my assumptions, and you are looking at my attempt to do so.
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
WriteBatch stores a list of Morton-packed keys in a single RPC call,
eliminating per-key RPC overhead. Semantically equivalent to calling
Write for each key.
*/
func (graph *GraphServer) WriteBatch(ctx context.Context, call Graph_writeBatch) error {
	keyList, err := call.Args().Keys()
	if err != nil {
		return err
	}

	graph.mu.Lock()
	defer graph.mu.Unlock()

	for idx := 0; idx < keyList.Len(); idx++ {
		key := keyList.At(idx)
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
	}

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
RecursiveFold builds a persistent hierarchical graph by discovering shared
structural invariants across values. Instead of an arbitrary midpoint split,
merge ordering is determined by Jaccard similarity (topology-guided): the
most similar pairs fold first, extracting their shared label (AND) and
directional residues (Hole). The residues are then folded recursively at
deeper levels until no shared structure remains.

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
func (graph *GraphServer) RecursiveFold(data []primitive.Value) {
	graph.foldGraph = graph.foldGraph[:0]
	graph.recursiveFoldLevel(data, 0)
}

/*
recursiveFoldLevel executes one level of topology-guided folding.
Precomputes all pairwise Jaccard similarities, sorts by decreasing
similarity, then processes merges via UnionFind. Shared structure
(AND) becomes fold labels; residues (Hole) are collected and
recursed on at the next depth level.
*/
func (graph *GraphServer) recursiveFoldLevel(data []primitive.Value, depth int) {
	if len(data) <= 1 {
		return
	}

	pairs := graph.computeSimilarityPairs(data)

	if len(pairs) == 0 {
		return
	}

	sort.Slice(pairs, func(a, b int) bool {
		return pairs[a].similarity > pairs[b].similarity
	})

	uf := topology.NewUnionFind(len(data))
	ids := make([]int32, len(data))

	for idx := range data {
		ids[idx] = uf.MakeSet()
	}

	reps := make(map[int32]primitive.Value, len(data))

	for idx := range data {
		reps[ids[idx]] = data[idx]
	}

	residues := make([]primitive.Value, 0, len(data))

	for _, pair := range pairs {
		rootA := uf.Find(ids[pair.indexA])
		rootB := uf.Find(ids[pair.indexB])

		if rootA == rootB {
			continue
		}

		valA := reps[rootA]
		valB := reps[rootB]

		label := errnie.Guard(graph.state, func() (primitive.Value, error) {
			return valA.AND(valB)
		})

		if graph.state.Failed() {
			return
		}

		if label.CoreActiveCount() == 0 {
			continue
		}

		resA := errnie.Guard(graph.state, func() (primitive.Value, error) {
			return valA.Hole(label)
		})

		resB := errnie.Guard(graph.state, func() (primitive.Value, error) {
			return valB.Hole(label)
		})

		if graph.state.Failed() {
			return
		}

		lblPhase, _ := label.RotationSeed()
		resAPhase, _ := resA.RotationSeed()
		resBPhase, _ := resB.RotationSeed()

		label.SetTrajectory(
			numeric.Phase(lblPhase),
			numeric.Phase(resAPhase),
		)

		label.SetGuardRadius(uint8(label.CoreActiveCount() % 256))

		if resB.CoreActiveCount() > 0 {
			label.SetTrajectory(
				numeric.Phase(lblPhase),
				numeric.Phase(resBPhase),
			)
		}

		graph.foldGraph = append(graph.foldGraph, FoldNode{
			Label:     label,
			ResidueA:  resA,
			ResidueB:  resB,
			SourceA:   pair.indexA,
			SourceB:   pair.indexB,
			Threshold: pair.similarity,
			Depth:     depth,
		})

		graph.emitFoldLabel(label, depth, "", data[pair.indexA].Bin(), 2)

		uf.Union(ids[pair.indexA], ids[pair.indexB])
		newRoot := uf.Find(ids[pair.indexA])

		merged := errnie.Guard(graph.state, func() (primitive.Value, error) {
			return valA.OR(valB)
		})

		if graph.state.Failed() {
			return
		}

		delete(reps, rootA)
		delete(reps, rootB)
		reps[newRoot] = merged

		if resA.CoreActiveCount() > 0 {
			residues = append(residues, resA)
		}

		if resB.CoreActiveCount() > 0 {
			residues = append(residues, resB)
		}
	}

	if len(residues) > 1 {
		graph.recursiveFoldLevel(residues, depth+1)
	}
}

/*
computeSimilarityPairs precomputes Jaccard similarity for all value pairs.
Only pairs with non-zero similarity are returned, since zero-similarity
pairs cannot produce a meaningful fold label.
*/
func (graph *GraphServer) computeSimilarityPairs(data []primitive.Value) []similarityPair {
	capacity := len(data) * (len(data) - 1) / 2
	pairs := make([]similarityPair, 0, capacity)

	for outer := 0; outer < len(data); outer++ {
		for inner := outer + 1; inner < len(data); inner++ {
			sim := topology.JaccardSimilarity(data[outer], data[inner])

			if sim > 0 {
				pairs = append(pairs, similarityPair{
					indexA:     outer,
					indexB:     inner,
					similarity: sim,
				})
			}
		}
	}

	return pairs
}

/*
FoldLookup returns fold nodes whose labels share non-zero structural
overlap with the query value. This is the graph-structural retrieval
path used during multi-stage prompt resolution.
*/
func (graph *GraphServer) FoldLookup(query primitive.Value) []FoldNode {
	graph.mu.RLock()
	defer graph.mu.RUnlock()

	results := make([]FoldNode, 0)

	for idx := range graph.foldGraph {
		node := &graph.foldGraph[idx]

		overlap := errnie.Guard(graph.state, func() (primitive.Value, error) {
			return query.AND(node.Label)
		})

		if graph.state.Failed() {
			graph.state.Reset()
			continue
		}

		if overlap.CoreActiveCount() > 0 {
			results = append(results, *node)
		}
	}

	return results
}

/*
FoldGraph returns the persistent fold hierarchy for inspection or
serialization. Callers must not mutate the returned slice.
*/
func (graph *GraphServer) FoldGraph() []FoldNode {
	graph.mu.RLock()
	defer graph.mu.RUnlock()

	return graph.foldGraph
}

/*
ExactContinuation returns the exact remainder for the first row whose prefix
matches the prompt Values byte-for-byte across core blocks of each Value.
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
valuesEqual compares each core block with no fuzzy matching.
*/
func (graph *GraphServer) valuesEqual(
	left primitive.Value,
	right primitive.Value,
) bool {
	for blockIndex := 0; blockIndex < config.CoreBlocks; blockIndex++ {
		if left.Block(blockIndex) != right.Block(blockIndex) {
			return false
		}
	}

	return true
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
			Theta:      float64(label.CoreActiveCount()) / float64(numeric.CoreBits),
			ParentBin:  parentBin,
			ChildCount: childCount,
			Density:    float64(label.CoreActiveCount()) / float64(numeric.CoreBits),
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
