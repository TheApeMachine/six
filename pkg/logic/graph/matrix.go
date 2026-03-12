package graph

import (
	"context"
	"fmt"
	"math"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/console"
	"github.com/theapemachine/six/pkg/data"
	"github.com/theapemachine/six/pkg/geometry"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/telemetry"
	"github.com/theapemachine/six/pkg/validate"
)

/*
MatrixServer implements the Cap'n Proto RPC interface for the logic graph.
It acts as the continuous reasoning engine (Cortex) evaluating geometric
vector states via hardware POPCNT (XOR cancellation).
*/
type MatrixServer struct {
	mu  sync.RWMutex
	ctx context.Context

	broadcast     *pool.BroadcastGroup
	workerPool    *pool.Pool
	rpcConn       *rpc.Conn
	sink          *telemetry.Sink
	spatialLookup func(context.Context, data.Chord_List) ([][]data.Chord, error)
}

/*
MatrixServerOpt configures MatrixServer.
*/
type MatrixServerOpt func(*MatrixServer)

/*
NewMatrixServer creates the RPC server for the logic graph matrix.
*/
func NewMatrixServer(opts ...MatrixServerOpt) *MatrixServer {
	matrix := &MatrixServer{
		sink: telemetry.NewSink(),
	}

	for _, opt := range opts {
		opt(matrix)
	}

	validate.Require(map[string]any{
		"workerPool": matrix.workerPool,
		"broadcast":  matrix.broadcast,
		"ctx":        matrix.ctx,
	})

	return matrix
}

/*
Announce exports the server as an RPC bootstrap capability over an in-memory
pipe, then broadcasts the client-side net.Conn so other systems can connect.
*/
func (matrix *MatrixServer) Announce() {
	console.Info("Announcing Matrix")

	serverSide, clientSide := net.Pipe()
	client := Matrix_ServerToClient(matrix)

	matrix.rpcConn = rpc.NewConn(rpc.NewStreamTransport(serverSide), &rpc.Options{
		BootstrapClient: capnp.Client(client),
	})

	matrix.broadcast.Send(&pool.Result{
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
func (matrix *MatrixServer) Receive(result *pool.Result) {
	if result == nil || result.Value == nil {
		return
	}

	if pv, ok := result.Value.(pool.PoolValue[func(context.Context, data.Chord_List) ([][]data.Chord, error)]); ok {
		if pv.Key == "spatial_lookup" {
			matrix.mu.Lock()
			matrix.spatialLookup = pv.Value
			matrix.mu.Unlock()
		}
	}
}

func (matrix *MatrixServer) Prompt(ctx context.Context, call Matrix_prompt) error {
	params := call.Args()
	chords, err := params.Chords()

	if err != nil {
		return console.Error(err)
	}

	res, err := call.AllocResults()

	if err != nil {
		return console.Error(err)
	}

	return matrix.prompt(ctx, chords, res)
}

func (matrix *MatrixServer) Done(ctx context.Context, call Matrix_done) error {
	return nil
}

/*
Evaluate sweeps a prompt chord against a contiguous path matrix
via XOR + POPCNT, returning the best index, its residue energy,
and the residue chord itself.
*/
func (matrix *MatrixServer) Evaluate(prompt data.Chord, paths []data.Chord) (bestIdx int, lowestEnergy int, residue data.Chord) {
	lowestEnergy = math.MaxInt32
	bestIdx = -1

	for i, path := range paths {
		res := prompt.XOR(path)
		energy := res.ActiveCount()

		if energy < lowestEnergy {
			lowestEnergy = energy
			bestIdx = i
			residue = res
		}
	}

	return bestIdx, lowestEnergy, residue
}

func (matrix *MatrixServer) prompt(
	ctx context.Context,
	chords data.Chord_List,
	res Matrix_prompt_Results,
) error {
	matrix.mu.RLock()
	lookup := matrix.spatialLookup
	matrix.mu.RUnlock()

	if lookup == nil {
		slice, err := data.ChordListToSlice(chords)

		if err != nil {
			return console.Error(err)
		}

		return matrix.writePaths(res, [][]data.Chord{slice})
	}

	pathsData, err := lookup(ctx, chords)

	if err != nil {
		return console.Error(err)
	}

	// Aggregate context chord: Chord(Sequence) = A ⊕ B ⊕ C
	contextChord, err := data.NewChord(res.Segment())

	if err != nil {
		return console.Error(err)
	}

	for i := 0; i < chords.Len(); i++ {
		contextChord = contextChord.XOR(chords.At(i))
	}

	for i, candidates := range pathsData {
		bestIdx, _, _ := matrix.Evaluate(contextChord, candidates)

		if bestIdx == -1 {
			pathsData[i] = nil
			continue
		}

		pathsData[i] = []data.Chord{candidates[bestIdx]}
	}

	matrix.RecursiveFold(pathsData, 0, -1)

	return matrix.writePaths(res, pathsData)
}

func (matrix *MatrixServer) writePaths(res Matrix_prompt_Results, paths [][]data.Chord) error {
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
RecursiveFold fractures geometric sequences into an isolated hierarchy of labels
connected by phase rotations (the "arrow of time"), firing pool jobs recursively.
*/
func (matrix *MatrixServer) RecursiveFold(sequences [][]data.Chord, level int, parentBin int) {
	if len(sequences) == 0 {
		return
	}

	// 1. Structural GCD
	labelChord := extractSharedInvariant(sequences)

	if labelChord.ActiveCount() == 0 {
		return
	}

	// 2. Derive Macroscopic Arrow of Time Pointer
	ei := geometry.NewEigenMode()
	theta, _ := ei.PhaseForChord(&labelChord) // The edge!

	labelBin := data.ChordBin(&labelChord)

	// 3. Extract the unique residues
	var uniqueResidues [][]data.Chord

	for _, seq := range sequences {
		residue := xorSequence(seq, labelChord)

		if len(residue) > 0 {
			uniqueResidues = append(uniqueResidues, residue)
		}
	}

	// Emit fold telemetry
	matrix.sink.Emit(telemetry.Event{
		Component: "Cortex",
		Action:    "Fold",
		Data: telemetry.EventData{
			Bin:        labelBin,
			Level:      level,
			Theta:      theta,
			ParentBin:  parentBin,
			ChildCount: len(uniqueResidues),
			ActiveBits: data.ChordPrimeIndices(&labelChord),
			Density:    labelChord.ShannonDensity(),
		},
	})

	// 4. Recurse via Pool
	for i, resSeq := range uniqueResidues {
		jobID := fmt.Sprintf("fold-level-%d-theta-%f-seq-%d", level, theta, i)

		matrix.workerPool.Schedule(jobID, func() (any, error) {
			matrix.RecursiveFold([][]data.Chord{resSeq}, level+1, labelBin)
			return nil, nil
		})
	}
}

/*
MatrixWithContext injects a context.
*/
func MatrixWithContext(ctx context.Context) MatrixServerOpt {
	return func(matrix *MatrixServer) {
		matrix.ctx = ctx
	}
}

/*
MatrixWithBroadcast injects the broadcast group.
*/
func MatrixWithBroadcast(broadcast *pool.BroadcastGroup) MatrixServerOpt {
	return func(matrix *MatrixServer) {
		matrix.broadcast = broadcast
	}
}

/*
MatrixWithPool injects the shared worker pool.
*/
func MatrixWithPool(p *pool.Pool) MatrixServerOpt {
	return func(matrix *MatrixServer) {
		matrix.workerPool = p
	}
}
