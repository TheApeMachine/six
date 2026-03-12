package lsm

import (
	"context"
	"math"
	"net"
	"sort"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/data"
	"github.com/theapemachine/six/pkg/geometry"
	"github.com/theapemachine/six/pkg/pool"
	"github.com/theapemachine/six/pkg/process"
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

	ctx       context.Context
	broadcast *pool.BroadcastGroup
	conn      *rpc.Conn
}

type spatialIndexOpts func(*SpatialIndexServer)

func NewSpatialIndexServer(opts ...spatialIndexOpts) *SpatialIndexServer {
	idx := &SpatialIndexServer{}

	for _, opt := range opts {
		opt(idx)
	}

	return idx
}

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

func (idx *SpatialIndexServer) Receive(_ *pool.Result) {}

func (idx *SpatialIndexServer) calcMorton(chord *data.Chord) uint64 {
	ei := geometry.NewEigenMode()
	theta, phi := ei.PhaseForChord(chord)
	r := float64(chord.ActiveCount()) * 10.0

	cellSize := 0.1
	offset := 1000000.0

	x := r * math.Sin(phi) * math.Cos(theta)
	y := r * math.Sin(phi) * math.Sin(theta)
	z := r * math.Cos(phi)

	ix := uint32(math.Floor(x/cellSize) + offset)
	iy := uint32(math.Floor(y/cellSize) + offset)
	iz := uint32(math.Floor(z/cellSize) + offset)

	coder := process.NewMortonCoder()
	return coder.Encode3D(ix, iy, iz)
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

func (idx *SpatialIndexServer) insertSync(mortonKey uint64, tokenID uint32, value data.Chord) {
	newEntry := SpatialEntry{MortonKey: mortonKey, TokenID: tokenID, Value: value}

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

		end := i + window
		if end > len(level) {
			end = len(level)
		}

		for j := i; j < end; j++ {
			hits = append(hits, level[j].Value)
		}
	}
	return hits
}

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
	position := reqEdge.Position()
	tokenID := (uint32(left) << 24) | uint32(position)

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

	mortonKey := idx.calcMorton(&val)
	idx.insertSync(mortonKey, tokenID, val)

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
	defer idx.mu.RUnlock()

	paths := make([][]data.Chord, chords.Len())
	for i := 0; i < chords.Len(); i++ {
		chord := chords.At(i)

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

		mKey := idx.calcMorton(&val)
		// We sweep forward linearly for 10 items contiguous in Morton space to get all branches
		paths[i] = idx.sweepForward(mKey, 10)
	}

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
	// Not strictly aligned with Morton spatial traversal yet,
	// but kept for API compliance.
	res, err := call.AllocResults()

	if err != nil {
		return err
	}

	resList, err := data.NewChord_List(res.Segment(), 0)

	if err != nil {
		return err
	}

	return res.SetChords(resList)
}

func WithContext(ctx context.Context) spatialIndexOpts {
	return func(idx *SpatialIndexServer) {
		idx.ctx = ctx
	}
}

func WithBroadcastGroup(group *pool.BroadcastGroup) spatialIndexOpts {
	return func(idx *SpatialIndexServer) {
		idx.broadcast = group
	}
}
