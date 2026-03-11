package lsm

import (
	"context"
	"net"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/pool"
)

/*
SpatialIndexServer implements the Cap'n Proto RPC interface for the Lexicon.
Collision is Compression.
faces[left][right][position] -> chord
*/
type SpatialIndexServer struct {
	mu sync.RWMutex

	faces   [256][256][]data.Chord
	count   int
	reverse map[data.Chord]uint64

	ctx       context.Context
	broadcast *pool.BroadcastGroup
	conn      *rpc.Conn
}

type spatialIndexOpts func(*SpatialIndexServer)

/*
NewSpatialIndexServer creates the core state of the LSM.
*/
func NewSpatialIndexServer(opts ...spatialIndexOpts) *SpatialIndexServer {
	idx := &SpatialIndexServer{
		reverse: make(map[data.Chord]uint64),
	}

	for _, opt := range opts {
		opt(idx)
	}

	return idx
}

/*
Announce exports the server as an RPC bootstrap capability over an in-memory
pipe, then broadcasts the client-side net.Conn so other systems can connect
and resolve the SpatialIndex capability via Bootstrap.
*/
func (idx *SpatialIndexServer) Announce() {
	if idx.broadcast == nil {
		return
	}

	serverSide, clientSide := net.Pipe()

	client := SpatialIndex_ServerToClient(idx)

	idx.conn = rpc.NewConn(rpc.NewStreamTransport(serverSide), &rpc.Options{
		BootstrapClient: capnp.Client(client),
	})

	idx.broadcast.Send(&pool.Result{
		Value: pool.PoolValue[net.Conn]{
			Key:   "spatial_index",
			Value: clientSide,
		},
	})
}

/*
Receive implements the vm.System interface.
SpatialIndexServer does not consume broadcast messages; it only produces them.
*/
func (idx *SpatialIndexServer) Receive(_ *pool.Result) {}

func (idx *SpatialIndexServer) insertSync(key uint64, value data.Chord) {
	left := byte((key >> 56) & 0xFF)
	right := byte((key >> 48) & 0xFF)
	position := uint32(key & 0xFFFFFFFFFFFF)

	idx.mu.Lock()
	defer idx.mu.Unlock()

	edge := &idx.faces[left][right]
	if int(position) < len(*edge) && (*edge)[position].ActiveCount() > 0 {
		return
	}
	if int(position) >= len(*edge) {
		grown := make([]data.Chord, position+1)
		copy(grown, *edge)
		*edge = grown
	}

	(*edge)[position] = value
	idx.count++
	idx.reverse[value] = key
}

func (idx *SpatialIndexServer) Count() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.count
}

func (idx *SpatialIndexServer) lookup(tokenID uint64) data.Chord {
	left := byte((tokenID >> 56) & 0xFF)
	right := byte((tokenID >> 48) & 0xFF)
	position := uint32(tokenID & 0xFFFFFFFFFFFF)

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if int(position) >= len(idx.faces[left][right]) {
		return data.Chord{}
	}
	return idx.faces[left][right][position]
}

func (idx *SpatialIndexServer) ReverseLookup(chord data.Chord) uint64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.reverse[chord]
}

// --- RPC Interface Implementations ---

func (idx *SpatialIndexServer) Insert(ctx context.Context, call SpatialIndex_insert) error {
	params := call.Args()
	reqEdge, err := params.Edge()
	if err != nil {
		return err
	}

	reqChord, err := reqEdge.Chord()
	if err != nil {
		return err
	}

	left := reqEdge.Left()
	right := reqEdge.Right()
	position := reqEdge.Position()

	val, err := data.NewChord(reqChord.Segment())

	if err != nil {
		return err
	}

	val.SetC0(reqChord.C0())
	val.SetC1(reqChord.C1())
	val.SetC2(reqChord.C2())
	val.SetC3(reqChord.C3())
	val.SetC4(reqChord.C4())
	val.SetC5(reqChord.C5())
	val.SetC6(reqChord.C6())
	val.SetC7(reqChord.C7())

	key := (uint64(left) << 56) | (uint64(right) << 48) | uint64(position)

	idx.insertSync(key, val)
	return nil
}

func (idx *SpatialIndexServer) Done(ctx context.Context, call SpatialIndex_done) error {
	return nil
}

func (idx *SpatialIndexServer) Lookup(ctx context.Context, call SpatialIndex_lookup) error {
	params := call.Args()
	chords, err := params.Chords()
	if err != nil {
		return err
	}

	idx.mu.RLock()
	paths := make([][]data.Chord, chords.Len())
	for i := 0; i < chords.Len(); i++ {
		chord := chords.At(i)
		
		// Use the value type Chord directly to perform the reverse lookup
		val, err := data.NewChord(chord.Segment())
		if err != nil {
			return err
		}
		val.SetC0(chord.C0())
		val.SetC1(chord.C1())
		val.SetC2(chord.C2())
		val.SetC3(chord.C3())
		val.SetC4(chord.C4())
		val.SetC5(chord.C5())
		val.SetC6(chord.C6())
		val.SetC7(chord.C7())

		key, exists := idx.reverse[val]
		if !exists {
			continue
		}

		right := byte((key >> 48) & 0xFF)
		position := uint32(key & 0xFFFFFFFFFFFF)

		var transitions []data.Chord
		for r := range 256 {
			edgeSlice := idx.faces[right][r]
			if int(position+1) < len(edgeSlice) && edgeSlice[position+1].ActiveCount() > 0 {
				transitions = append(transitions, edgeSlice[position+1])
			}
		}
		paths[i] = transitions
	}
	idx.mu.RUnlock()

	res, err := call.AllocResults()
	if err != nil {
		return err
	}

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

func (idx *SpatialIndexServer) QueryTransitions(ctx context.Context, call SpatialIndex_queryTransitions) error {
	params := call.Args()
	left := params.Left()
	position := params.Position()

	idx.mu.RLock()
	var results []data.Chord
	for right := range 256 {
		edge := idx.faces[left][right]
		if int(position) < len(edge) && edge[position].ActiveCount() > 0 {
			results = append(results, edge[position])
		}
	}
	idx.mu.RUnlock()

	res, err := call.AllocResults()
	if err != nil {
		return err
	}
	list, err := res.NewChords(int32(len(results)))
	if err != nil {
		return err
	}

	for i, c := range results {
		resChord := list.At(i)
		resChord.SetC0(c.C0())
		resChord.SetC1(c.C1())
		resChord.SetC2(c.C2())
		resChord.SetC3(c.C3())
		resChord.SetC4(c.C4())
		resChord.SetC5(c.C5())
		resChord.SetC6(c.C6())
		resChord.SetC7(c.C7())
	}
	return nil
}

/*
SpatialIndexWithContext sets a cancellable context on the server.
*/
func SpatialIndexWithContext(ctx context.Context) spatialIndexOpts {
	return func(idx *SpatialIndexServer) {
		idx.ctx = ctx
	}
}

/*
SpatialIndexWithBroadcast injects the broadcast group.
*/
func SpatialIndexWithBroadcast(broadcast *pool.BroadcastGroup) spatialIndexOpts {
	return func(idx *SpatialIndexServer) {
		idx.broadcast = broadcast
	}
}
