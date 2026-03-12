package lsm

import (
	"context"
	"math"
	"net"
	"sort"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/console"
	"github.com/theapemachine/six/pkg/data"
	"github.com/theapemachine/six/pkg/geometry"
	"github.com/theapemachine/six/pkg/pool"
)

type SpatialEntry struct {
	MortonKey uint64
	TokenID   uint32
	Value     data.Chord
}

/*
SpatialIndexServer implements the Cap'n Proto RPC interface for the Lexicon.
Collision is Compression.
It forms a Radix Forest using a sorted Morton Key LSM layout.
*/
type SpatialIndexServer struct {
	mu sync.RWMutex

	levels [][]SpatialEntry
	count  int

	ctx        context.Context
	broadcast  *pool.BroadcastGroup
	conn       *rpc.Conn
	clientConn *rpc.Conn

	ei    *geometry.EigenMode
	coder *data.MortonCoder
}

type spatialIndexOpts func(*SpatialIndexServer)

func NewSpatialIndexServer(opts ...spatialIndexOpts) *SpatialIndexServer {
	idx := &SpatialIndexServer{
		ei:    geometry.NewEigenMode(),
		coder: data.NewMortonCoder(),
	}

	for _, opt := range opts {
		opt(idx)
	}

	return idx
}

func (idx *SpatialIndexServer) Announce() {
	console.Info("Announcing SpatialIndexServer")

	if idx.broadcast == nil {
		return
	}

	serverSide, clientSide := net.Pipe()
	client := SpatialIndex_ServerToClient(idx)

	idx.conn = rpc.NewConn(rpc.NewStreamTransport(serverSide), &rpc.Options{
		BootstrapClient: capnp.Client(client),
	})

	conn := rpc.NewConn(rpc.NewStreamTransport(clientSide), nil)
	idx.clientConn = conn

	announceSpatialClient(idx.broadcast, SpatialIndex(conn.Bootstrap(idx.ctx)))
}

func (idx *SpatialIndexServer) Receive(_ *pool.Result) {}

func (idx *SpatialIndexServer) calcMorton(chord *data.Chord) uint64 {
	theta, phi := idx.ei.PhaseForChord(chord)
	r := float64(chord.ActiveCount()) * 10.0

	cellSize := 0.1
	offset := 1000000.0

	x := r * math.Sin(phi) * math.Cos(theta)
	y := r * math.Sin(phi) * math.Sin(theta)
	z := r * math.Cos(phi)

	ix := uint32(math.Floor(x/cellSize) + offset)
	iy := uint32(math.Floor(y/cellSize) + offset)
	iz := uint32(math.Floor(z/cellSize) + offset)

	return idx.coder.Encode3D(ix, iy, iz)
}

func mergeSpatialEntries(a, b []SpatialEntry) []SpatialEntry {
	sizeA := len(a)
	sizeB := len(b)
	out := make([]SpatialEntry, 0, sizeA+sizeB)

	i, j := 0, 0

	for i < sizeA && j < sizeB {
		if a[i].MortonKey < b[j].MortonKey {
			out = append(out, a[i])
			i++
		} else if b[j].MortonKey < a[i].MortonKey {
			out = append(out, b[j])
			j++
		} else {
			if a[i].TokenID == b[j].TokenID {
				out = append(out, a[i])
				i++
				j++
			} else if a[i].TokenID < b[j].TokenID {
				out = append(out, a[i])
				i++
			} else {
				out = append(out, b[j])
				j++
			}
		}
	}

	for i < sizeA {
		out = append(out, a[i])
		i++
	}

	for j < sizeB {
		out = append(out, b[j])
		j++
	}

	return out
}

func (idx *SpatialIndexServer) insertSync(
	mortonKey uint64, tokenID uint32, value data.Chord,
) {
	newEntry := SpatialEntry{
		MortonKey: mortonKey,
		TokenID:   tokenID,
		Value:     value,
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	curr := []SpatialEntry{newEntry}
	level := 0

	for level < len(idx.levels) {
		if idx.levels[level] == nil {
			break
		}
		curr = mergeSpatialEntries(idx.levels[level], curr)
		idx.levels[level] = nil
		level++
	}

	if level == len(idx.levels) {
		idx.levels = append(idx.levels, curr)
	} else {
		idx.levels[level] = curr
	}

	idx.count = 0

	for _, l := range idx.levels {
		idx.count += len(l)
	}
}

func (idx *SpatialIndexServer) Count() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.count
}

func (idx *SpatialIndexServer) sweepForward(mKey uint64, window int) []data.Chord {
	var hits []data.Chord

	for _, level := range idx.levels {
		if level == nil {
			continue
		}
		i := sort.Search(len(level), func(i int) bool {
			return level[i].MortonKey >= mKey
		})

		end := min(i+window, len(level))

		for j := i; j < end; j++ {
			hits = append(hits, level[j].Value)
		}
	}
	return hits
}

func (idx *SpatialIndexServer) Insert(ctx context.Context, call SpatialIndex_insert) error {
	args := call.Args()
	edge, err := args.Edge()
	if err != nil {
		return console.Error(err)
	}

	chord, err := edge.Chord()
	if err != nil {
		return console.Error(err)
	}

	chordVal, err := data.NewChord(chord.Segment())
	if err != nil {
		return console.Error(err)
	}

	chordVal.CopyFrom(chord)
	tokenID := (uint32(edge.Left()) << 24) | uint32(edge.Position())
	idx.insertSync(idx.calcMorton(&chordVal), tokenID, chordVal)

	return nil
}

func (idx *SpatialIndexServer) Done(ctx context.Context, call SpatialIndex_done) error {
	return nil
}

/*
buildPaths sweeps each chord in chordList through the Morton-keyed spatial index
and returns the matching path slices. Holds the read lock only for the duration
of the sweep.
*/
func (idx *SpatialIndexServer) buildPaths(chordList data.Chord_List) ([][]data.Chord, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	paths := make([][]data.Chord, chordList.Len())

	for i := 0; i < chordList.Len(); i++ {
		chord := chordList.At(i)

		chordVal, err := data.NewChord(chord.Segment())
		if err != nil {
			return nil, console.Error(err)
		}

		chordVal.CopyFrom(chord)
		paths[i] = idx.sweepForward(idx.calcMorton(&chordVal), 10)
	}

	return paths, nil
}

/*
writeLookupResults serialises a slice of path slices into the Cap'n Proto response.
*/
func (idx *SpatialIndexServer) writeLookupResults(
	call SpatialIndex_lookup, paths [][]data.Chord,
) error {
	res, err := call.AllocResults()
	if err != nil {
		return console.Error(err)
	}

	pathsList, err := res.NewPaths(int32(len(paths)))
	if err != nil {
		return console.Error(err)
	}

	for i := 0; i < len(paths); i++ {
		list, err := data.NewChord_List(res.Segment(), int32(len(paths[i])))
		if err != nil {
			return console.Error(err)
		}

		for j, c := range paths[i] {
			el := list.At(j)
			el.CopyFrom(c)
		}

		if err := pathsList.Set(i, list.ToPtr()); err != nil {
			return console.Error(err)
		}
	}

	return nil
}

func (idx *SpatialIndexServer) Lookup(ctx context.Context, call SpatialIndex_lookup) error {
	chords, err := call.Args().Chords()
	if err != nil {
		return console.Error(err)
	}

	paths, err := idx.buildPaths(chords)
	if err != nil {
		return console.Error(err)
	}

	return idx.writeLookupResults(call, paths)
}

func (idx *SpatialIndexServer) QueryTransitions(
	ctx context.Context, call SpatialIndex_queryTransitions,
) error {
	res, err := call.AllocResults()
	if err != nil {
		return console.Error(err)
	}

	list, err := data.NewChord_List(res.Segment(), 0)
	if err != nil {
		return console.Error(err)
	}

	return res.SetChords(list)
}

func WithContext(ctx context.Context) spatialIndexOpts {
	return func(idx *SpatialIndexServer) {
		idx.ctx = ctx
	}
}

func WithBroadcast(group *pool.BroadcastGroup) spatialIndexOpts {
	return func(idx *SpatialIndexServer) {
		idx.broadcast = group
	}
}
