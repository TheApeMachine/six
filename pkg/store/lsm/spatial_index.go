package lsm

import (
	"context"
	"net"
	"sort"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/console"
	"github.com/theapemachine/six/pkg/data"
	"github.com/theapemachine/six/pkg/pool"
)

var morton = data.NewMortonCoder()

/*
SpatialEntry stores a single edge in the radix forest.
Key is a MortonCoder-packed uint64: Pack(position, symbol).
The value is the full chunk chord for the token stored at that key.
On collision the existing entry wins — data is discarded, not merged.
*/
type SpatialEntry struct {
	Key   uint64
	Value data.Chord
}

/*
SpatialIndexServer implements the Cap'n Proto RPC interface for the Lexicon.
Keys are packed via MortonCoder.Pack(position, symbol).
Collision is Discard.
*/
type SpatialIndexServer struct {
	mu sync.RWMutex

	entries map[uint64]data.Chord
	count   int

	ctx        context.Context
	broadcast  *pool.BroadcastGroup
	conn       *rpc.Conn
	clientConn *rpc.Conn
}

type spatialIndexOpts func(*SpatialIndexServer)

func NewSpatialIndexServer(opts ...spatialIndexOpts) *SpatialIndexServer {
	idx := &SpatialIndexServer{
		entries: make(map[uint64]data.Chord),
	}

	for _, opt := range opts {
		opt(idx)
	}

	return idx
}

/*
Start implements the vm.System interface.
*/
func (idx *SpatialIndexServer) Start(workerPool *pool.Pool, broadcast *pool.BroadcastGroup) {
	idx.broadcast = broadcast
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

/*
insertSync stores a token. On collision the existing entry wins.
*/
func (idx *SpatialIndexServer) insertSync(key uint64, value data.Chord) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if _, exists := idx.entries[key]; exists {
		return
	}

	idx.entries[key] = value
	idx.count++
}

func (idx *SpatialIndexServer) Count() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.count
}

/*
Ready reports whether the spatial index has been populated.
*/
func (idx *SpatialIndexServer) Ready() bool {
	return idx.Count() > 0
}

/*
entriesAtPosition returns all entries at the given position
(lower 32 bits of the Morton key).
*/
func (idx *SpatialIndexServer) entriesAtPosition(position uint32) []SpatialEntry {
	var hits []SpatialEntry

	for key, value := range idx.entries {
		pos, _ := morton.Unpack(key)

		if pos == position {
			hits = append(hits, SpatialEntry{Key: key, Value: value})
		}
	}

	return hits
}

/*
branchesFrom returns all entries at positions greater than the given one.
*/
func (idx *SpatialIndexServer) branchesFrom(position uint32) []data.Chord {
	var hits []data.Chord

	for key, value := range idx.entries {
		pos, _ := morton.Unpack(key)

		if pos > position {
			hits = append(hits, value)
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

	_, ownSeg, err := capnp.NewMessage(capnp.MultiSegment(nil))

	if err != nil {
		return console.Error(err)
	}

	chordVal, err := data.NewChord(ownSeg)

	if err != nil {
		return console.Error(err)
	}

	chordVal.CopyFrom(chord)
	key := morton.Pack(edge.Position(), edge.Left())
	idx.insertSync(key, chordVal)

	return nil
}

func (idx *SpatialIndexServer) Done(ctx context.Context, call SpatialIndex_done) error {
	return nil
}

/*
buildPaths walks each prompt chord through the radix trie.
*/
func (idx *SpatialIndexServer) buildPaths(chordList data.Chord_List) ([][]data.Chord, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	paths := make([][]data.Chord, chordList.Len())

	for i := 0; i < chordList.Len(); i++ {
		promptChord := chordList.At(i)
		promptActive := promptChord.ActiveCount()

		deepest := uint32(0)
		matched := false

		for pos := uint32(0); ; pos++ {
			entries := idx.entriesAtPosition(pos)

			if len(entries) == 0 {
				break
			}

			foundAtPos := false

			for _, entry := range entries {
				sim := data.ChordSimilarity(&entry.Value, &promptChord)

				if sim == entry.Value.ActiveCount() && sim > 0 && sim <= promptActive {
					foundAtPos = true
					deepest = pos
					matched = true
				}
			}

			if !foundAtPos {
				break
			}
		}

		if matched {
			paths[i] = idx.branchesFrom(deepest)
		}
	}

	return paths, nil
}

/*
writeLookupResults serialises path slices into the Cap'n Proto response.
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

	for i := range paths {
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

func (idx *SpatialIndexServer) Lookup(
	ctx context.Context,
	call SpatialIndex_lookup,
) error {
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

/*
QueryTransitions returns all chunk chords stored at the given
(left, position) key.
*/
func (idx *SpatialIndexServer) QueryTransitions(
	ctx context.Context, call SpatialIndex_queryTransitions,
) error {
	args := call.Args()
	left := args.Left()
	position := args.Position()

	key := morton.Pack(position, left)

	idx.mu.RLock()

	var hits []data.Chord

	if value, exists := idx.entries[key]; exists {
		hits = append(hits, value)
	}

	idx.mu.RUnlock()

	res, err := call.AllocResults()

	if err != nil {
		return console.Error(err)
	}

	list, err := data.NewChord_List(res.Segment(), int32(len(hits)))

	if err != nil {
		return console.Error(err)
	}

	for i, chord := range hits {
		el := list.At(i)
		el.CopyFrom(chord)
	}

	return res.SetChords(list)
}

func WithContext(ctx context.Context) spatialIndexOpts {
	return func(idx *SpatialIndexServer) {
		idx.ctx = ctx
	}
}

/*
Decode reconstructs byte sequences from result chords.
Uses MortonCoder.Unpack to extract (position, symbol) from each key.
*/
func (idx *SpatialIndexServer) Decode(chords []data.Chord) [][]byte {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	results := make([][]byte, 0, len(chords))

	for _, chord := range chords {
		active := chord.ActiveCount()

		if active == 0 {
			continue
		}

		type hit struct {
			pos    uint32
			symbol byte
		}

		var matched []hit

		for key, value := range idx.entries {
			sim := data.ChordSimilarity(&value, &chord)

			if sim == value.ActiveCount() && sim > 0 && sim <= active {
				pos, symbol := morton.Unpack(key)
				matched = append(matched, hit{pos, symbol})
			}
		}

		if len(matched) == 0 {
			continue
		}

		sort.Slice(matched, func(i, j int) bool {
			return matched[i].pos < matched[j].pos
		})

		buf := make([]byte, 0, len(matched))

		for _, m := range matched {
			buf = append(buf, m.symbol)
		}

		results = append(results, buf)
	}

	return results
}
