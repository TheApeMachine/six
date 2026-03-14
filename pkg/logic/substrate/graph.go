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
	"github.com/theapemachine/six/pkg/store/lsm"
	"github.com/theapemachine/six/pkg/system/console"
	"github.com/theapemachine/six/pkg/system/pool"
	"github.com/theapemachine/six/pkg/telemetry"
	"github.com/theapemachine/six/pkg/validate"
)

/*
Graph implements the Cap'n Proto RPC interface for the logic graph.
It acts as the continuous reasoning engine (Cortex) evaluating geometric
vector states via hardware POPCNT (XOR cancellation).
*/
type GraphServer struct {
	mu  sync.RWMutex
	ctx context.Context

	broadcast     *pool.BroadcastGroup
	workerPool    *pool.Pool
	rpcConn       *rpc.Conn
	sink          *telemetry.Sink
	spatialLookup func(context.Context, data.Chord_List) ([][]data.Chord, [][]data.Chord, error)
}

/*
GraphOpt configures Graph.
*/
type GraphOpt func(*GraphServer)

/*
NewGraphServer creates the RPC server for the logic graph.
*/
func NewGraphServer(opts ...GraphOpt) *GraphServer {
	graph := &GraphServer{
		sink: telemetry.NewSink(),
	}

	for _, opt := range opts {
		opt(graph)
	}

	if graph.ctx == nil {
		graph.ctx = context.Background()
	}

	validate.Require(map[string]any{
		"ctx": graph.ctx,
	})

	return graph
}

/*
Start implements the vm.System interface.
*/
func (graph *GraphServer) Start(
	workerPool *pool.Pool, broadcast *pool.BroadcastGroup,
) {
	graph.workerPool = workerPool
	graph.broadcast = broadcast
}

/*
Announce exports the server as an RPC bootstrap capability over an in-memory
pipe, then broadcasts the client-side net.Conn so other systems can connect.
*/
func (graph *GraphServer) Announce() {
	console.Info("Announcing Graph")

	serverSide, clientSide := net.Pipe()
	client := Graph_ServerToClient(graph)

	graph.rpcConn = rpc.NewConn(rpc.NewStreamTransport(serverSide), &rpc.Options{
		BootstrapClient: capnp.Client(client),
	})

	graph.broadcast.Send(&pool.Result{
		Value: pool.PoolValue[net.Conn]{
			Key:   "matrix",
			Value: clientSide,
		},
	})
}

/*
Receive implements the vm.System interface.
Picks up the client-side net.Conn from the broadcast bus for the LSM.
*/
func (graph *GraphServer) Receive(result *pool.Result) {
	if result == nil || result.Value == nil {
		return
	}

	if pv, ok := result.Value.(pool.PoolValue[lsm.SpatialLookupFunc]); ok {
		if pv.Key == lsm.SpatialLookupKey {
			graph.mu.Lock()
			graph.spatialLookup = pv.Value
			graph.mu.Unlock()
		}
	}
}

func (graph *GraphServer) Prompt(ctx context.Context, call Graph_prompt) error {
	params := call.Args()
	chords, err := params.Chords()

	if err != nil {
		return console.Error(err)
	}

	res, err := call.AllocResults()

	if err != nil {
		return console.Error(err)
	}

	return graph.prompt(ctx, chords, res)
}

func (graph *GraphServer) Done(ctx context.Context, call Graph_done) error {
	return nil
}

/*
Evaluate sweeps a prompt chord against a contiguous path matrix
via XOR + POPCNT, returning the best index, its residue energy,
and the residue chord itself.
*/
func (graph *GraphServer) Evaluate(
	prompt data.Chord, paths []data.Chord,
	interest *data.Chord, danger *data.Chord,
) (bestIdx int, lowestEnergy int, residue data.Chord) {
	lowestEnergy = math.MaxInt32
	bestIdx = -1

	for i, path := range paths {
		res := prompt.XOR(path)
		energy := res.ActiveCount()

		// Resonance Bias (The Gravity Field)
		if interest != nil {
			resonance := path.AND(*interest)
			energy -= resonance.ActiveCount() // Lower effective entropy (better)
		}

		// Damping Danger Zones
		if danger != nil {
			punish := path.AND(*danger)
			energy += punish.ActiveCount() // Higher effective entropy (worse)
		}

		if energy < lowestEnergy {
			lowestEnergy = energy
			bestIdx = i
			residue = res
		}
	}

	return bestIdx, lowestEnergy, residue
}

func (graph *GraphServer) prompt(
	ctx context.Context,
	chords data.Chord_List,
	res Graph_prompt_Results,
) error {
	graph.mu.RLock()
	lookup := graph.spatialLookup
	graph.mu.RUnlock()

	if lookup == nil {
		slice, err := data.ChordListToSlice(chords)

		if err != nil {
			return console.Error(err)
		}

		return graph.writePaths(res, [][]data.Chord{slice})
	}

	pathsData, metaPathsData, err := lookup(ctx, chords)

	if err != nil {
		return console.Error(err)
	}

	// Aggregate context chord: Chord(Sequence) = A ⊕ B ⊕ C
	contextChord, err := data.NewChord(res.Segment())

	if err != nil {
		return console.Error(err)
	}

	for i := 0; i < chords.Len(); i++ {
		c := chords.At(i)
		contextChord = contextChord.XOR(c)
	}

	for i, candidates := range pathsData {
		bestIdx, _, _ := graph.Evaluate(contextChord, candidates, nil, nil)

		if bestIdx == -1 {
			pathsData[i] = nil
			metaPathsData[i] = nil
			continue
		}

		pathsData[i] = []data.Chord{candidates[bestIdx]}
		if len(metaPathsData) > i && len(metaPathsData[i]) > bestIdx {
			metaPathsData[i] = []data.Chord{metaPathsData[i][bestIdx]}
		} else {
			metaPathsData[i] = nil
		}
	}

	graph.RecursiveFold(pathsData, metaPathsData, 0, -1)

	return graph.writePaths(res, pathsData)
}

func (graph *GraphServer) writePaths(res Graph_prompt_Results, paths [][]data.Chord) error {
	pathsList, err := res.NewPaths(int32(len(paths)))

	if err != nil {
		return console.Error(err)
	}

	seg := res.Segment()

	for i, pathChords := range paths {
		innerList, err := data.NewChord_List(seg, int32(len(pathChords)))

		if err != nil {
			return console.Error(err)
		}

		for j, pathChord := range pathChords {
			el := innerList.At(j)
			el.CopyFrom(pathChord)
		}

		if err := pathsList.Set(i, innerList.ToPtr()); err != nil {
			return console.Error(err)
		}
	}

	return nil
}

/*
RecursiveFold fractures geometric sequences into an isolated
hierarchy of labels connected by phase rotations (the "arrow of time"),
firing pool jobs recursively.
*/
func (graph *GraphServer) RecursiveFold(
	sequences [][]data.Chord,
	metaSequences [][]data.Chord,
	level int,
	parentBin int,
) {
	if len(sequences) == 0 || len(metaSequences) == 0 {
		return
	}

	// 1. Structural GCD
	labelDataChord := extractSharedInvariant(sequences)
	labelMetaChord := extractSharedInvariant(metaSequences)

	if labelDataChord.ActiveCount() == 0 {
		return
	}

	labelBin := data.ChordBin(&labelDataChord)

	// 2. EigenMode Phase Analysis
	ei := geometry.NewEigenMode()
	theta, _ := ei.PhaseForChord(&labelMetaChord)

	// 3. Extract the unique residues
	var uniqueResidues [][]data.Chord
	var uniqueMetaResidues [][]data.Chord

	for i, seq := range sequences {
		metaSeq := metaSequences[i]
		residue := xorSequence(seq, labelDataChord)
		metaResidue := xorSequence(metaSeq, labelMetaChord)

		if len(residue) > 0 {
			uniqueResidues = append(uniqueResidues, residue)
			uniqueMetaResidues = append(uniqueMetaResidues, metaResidue)
		}
	}

	// Emit fold telemetry
	graph.sink.Emit(telemetry.Event{
		Component: "Cortex",
		Action:    "Fold",
		Data: telemetry.EventData{
			Bin:        labelBin,
			Level:      level,
			ParentBin:  parentBin,
			ChildCount: len(uniqueResidues),
			ActiveBits: data.ChordPrimeIndices(&labelMetaChord),
			Density:    labelMetaChord.ShannonDensity(),
			Theta:      theta,
		},
	})

	// 4. Recurse via Pool
	for index, resSeq := range uniqueResidues {
		metaResSeq := uniqueMetaResidues[index]
		jobID := fmt.Sprintf("fold-level-%d-seq-%d", level, index)

		graph.workerPool.Schedule(jobID, func(ctx context.Context) (any, error) {
			graph.RecursiveFold(
				[][]data.Chord{resSeq},
				[][]data.Chord{metaResSeq},
				level+1,
				labelBin,
			)
			return nil, nil
		})
	}
}

/*
PromptChords performs the full Prompt→SpatialLookup→Evaluate→RecursiveFold
pipeline and returns the resulting paths as Go slices. Called by Machine.Prompt
to exercise the real system without capnp result allocation.
*/
func (graph *GraphServer) PromptChords(
	ctx context.Context, chords data.Chord_List,
) ([][]data.Chord, error) {
	graph.mu.RLock()
	lookup := graph.spatialLookup
	graph.mu.RUnlock()

	if lookup == nil {
		slice, err := data.ChordListToSlice(chords)

		if err != nil {
			return nil, console.Error(err)
		}

		return [][]data.Chord{slice}, nil
	}

	pathsData, metaPathsData, err := lookup(ctx, chords)

	if err != nil {
		return nil, console.Error(err)
	}

	contextChord := data.MustNewChord()

	for i := 0; i < chords.Len(); i++ {
		c := chords.At(i)
		contextChord = contextChord.XOR(c)
	}

	for i, candidates := range pathsData {
		bestIdx, _, _ := graph.Evaluate(contextChord, candidates, nil, nil)

		if bestIdx == -1 {
			pathsData[i] = nil
			metaPathsData[i] = nil
			continue
		}

		pathsData[i] = []data.Chord{candidates[bestIdx]}
		if len(metaPathsData) > i && len(metaPathsData[i]) > bestIdx {
			metaPathsData[i] = []data.Chord{metaPathsData[i][bestIdx]}
		} else {
			metaPathsData[i] = nil
		}
	}

	graph.RecursiveFold(pathsData, metaPathsData, 0, -1)

	return pathsData, nil
}

/*
GraphWithContext injects a context.
*/
func GraphWithContext(ctx context.Context) GraphOpt {
	return func(graph *GraphServer) {
		graph.ctx = ctx
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
