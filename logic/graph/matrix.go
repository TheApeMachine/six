package graph

import (
	"context"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/pool"
)

var spatialLookupMethod = capnp.Method{
	InterfaceID:   0xfdb082e626e1958b,
	MethodID:      2,
	InterfaceName: "store/lsm/spatial_index.capnp:SpatialIndex",
	MethodName:    "lookup",
}

/*
MatrixServer implements the Cap'n Proto RPC interface for the logic graph.
It acts as the continuous reasoning engine (Cortex) evaluating geometric 
vector states via hardware POPCNT (XOR cancellation).
*/
type MatrixServer struct {
	mu  sync.RWMutex
	ctx context.Context

	broadcast   *pool.BroadcastGroup
	rpcConn     *rpc.Conn
	spatialConn capnp.Client
}

/*
MatrixServerOpt configures MatrixServer.
*/
type MatrixServerOpt func(*MatrixServer)

/*
NewMatrixServer creates the RPC server for the logic graph matrix.
*/
func NewMatrixServer(opts ...MatrixServerOpt) *MatrixServer {
	matrix := &MatrixServer{}
	for _, opt := range opts {
		opt(matrix)
	}
	return matrix
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
Announce exports the server as an RPC bootstrap capability over an in-memory
pipe, then broadcasts the client-side net.Conn so other systems can connect.
*/
func (matrix *MatrixServer) Announce() {
	if matrix.broadcast == nil {
		return
	}

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

	if pv, ok := result.Value.(pool.PoolValue[net.Conn]); ok {
		if pv.Key == "spatial_index" {
			conn := rpc.NewConn(rpc.NewStreamTransport(pv.Value), nil)

			matrix.mu.Lock()
			matrix.spatialConn = conn.Bootstrap(matrix.ctx)
			matrix.mu.Unlock()
		}
	}
}

// --- RPC Interface Implementations ---

func (matrix *MatrixServer) Prompt(ctx context.Context, call Matrix_prompt) error {
	params := call.Args()
	chords, err := params.Chords()
	if err != nil {
		return err
	}

	res, err := call.AllocResults()
	if err != nil {
		return err
	}

	return matrix.prompt(ctx, chords, res)
}

func (matrix *MatrixServer) Done(ctx context.Context, call Matrix_done) error {
	return nil
}

func (matrix *MatrixServer) prompt(ctx context.Context, chords data.Chord_List, res Matrix_prompt_Results) error {
	matrix.mu.RLock()
	spatialConn := matrix.spatialConn
	matrix.mu.RUnlock()

	if !spatialConn.IsValid() {
		return matrix.writePaths(res, [][]data.Chord{chordsToSlice(chords)})
	}

	// Call LSM's lookup method dynamically
	future, release := spatialConn.SendCall(ctx, capnp.Send{
		Method:   spatialLookupMethod,
		ArgsSize: capnp.ObjectSize{PointerCount: 1},
		PlaceArgs: func(s capnp.Struct) error {
			innerList, err := data.NewChord_List(s.Segment(), int32(chords.Len()))
			if err != nil {
				return err
			}
			for i := 0; i < chords.Len(); i++ {
				src := chords.At(i)
				dst := innerList.At(i)
				dst.SetC0(src.C0())
				dst.SetC1(src.C1())
				dst.SetC2(src.C2())
				dst.SetC3(src.C3())
				dst.SetC4(src.C4())
				dst.SetC5(src.C5())
				dst.SetC6(src.C6())
				dst.SetC7(src.C7())
			}
			return s.SetPtr(0, innerList.ToPtr())
		},
	})
	defer release()

	lookupRes, err := future.Struct()
	if err != nil {
		return err // error calling spatial_index.lookup
	}

	// The struct is the Results of spatial_index.lookup: paths @0 :List(List(Chord))
	ptr, err := lookupRes.Ptr(0)
	if err != nil {
		return err
	}

	pathsList := capnp.PointerList(ptr.List()) // List of List(Chord)
	pathsData := make([][]data.Chord, pathsList.Len())

	// Create the aggregate context chord for geometrically solving the equation
	// Chord(Sequence) = A ⊕ B ⊕ C
	contextChord, _ := data.NewChord(res.Segment())
	for i := 0; i < chords.Len(); i++ {
		c := chords.At(i)
		contextChord = contextChord.XOR(c)
	}

	for i := 0; i < pathsList.Len(); i++ {
		pPtr, err := pathsList.At(i)
		if err != nil {
			return err
		}
		innerList := data.Chord_List(pPtr.List())

		// Evaluation: Score each candidate path by geometric residue
		bestPathIdx := -1
		minResidue := 1024 // Greater than max possible popcount (257)

		for j := 0; j < innerList.Len(); j++ {
			candidate := innerList.At(j)
			
			// Hardware POPCNT ( Context ⊕ PathMatrix[i] )
			residue := contextChord.XOR(candidate)
			score := residue.ActiveCount()


			if score < minResidue {
				minResidue = score
				bestPathIdx = j
			}
		}

		if bestPathIdx != -1 {
			c := innerList.At(bestPathIdx)
			// Need to instantiate a fresh Chord to return
			outChord, _ := data.NewChord(c.Segment())
			outChord.SetC0(c.C0())
			outChord.SetC1(c.C1())
			outChord.SetC2(c.C2())
			outChord.SetC3(c.C3())
			outChord.SetC4(c.C4())
			outChord.SetC5(c.C5())
			outChord.SetC6(c.C6())
			outChord.SetC7(c.C7())
			// We just return one specific best choice for each branch in the context!
			pathsData[i] = []data.Chord{outChord}
		}
	}

	return matrix.writePaths(res, pathsData)
}

func (matrix *MatrixServer) writePaths(res Matrix_prompt_Results, paths [][]data.Chord) error {
	pathsList, err := res.NewPaths(int32(len(paths)))
	if err != nil {
		return err
	}

	seg := res.Segment()
	for i, pathChords := range paths {
		innerList, err := data.NewChord_List(seg, int32(len(pathChords)))
		if err != nil {
			return err
		}
		for j := 0; j < len(pathChords); j++ {
			el := innerList.At(j)
			c := pathChords[j]
			el.SetC0(c.C0())
			el.SetC1(c.C1())
			el.SetC2(c.C2())
			el.SetC3(c.C3())
			el.SetC4(c.C4())
			el.SetC5(c.C5())
			el.SetC6(c.C6())
			el.SetC7(c.C7())
		}
		if err := pathsList.Set(i, innerList.ToPtr()); err != nil {
			return err
		}
	}

	return nil
}

func chordsToSlice(chords data.Chord_List) []data.Chord {
	out := make([]data.Chord, chords.Len())
	for i := 0; i < chords.Len(); i++ {
		c := chords.At(i)
		chord, err := data.NewChord(c.Segment())
		if err != nil {
			return nil
		}
		chord.SetC0(c.C0())
		chord.SetC1(c.C1())
		chord.SetC2(c.C2())
		chord.SetC3(c.C3())
		chord.SetC4(c.C4())
		chord.SetC5(c.C5())
		chord.SetC6(c.C6())
		chord.SetC7(c.C7())
		out[i] = chord
	}
	return out
}
