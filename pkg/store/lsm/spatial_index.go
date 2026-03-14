package lsm

import (
	"context"
	"net"
	"sort"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/system/console"
	"github.com/theapemachine/six/pkg/system/pool"
)

var morton = data.NewMortonCoder()

/*
SpatialEntry stores a single edge in the radix forest.
Key is a MortonCoder-packed uint64: Pack(position, symbol).
The data chord is deterministic (5 bits per byte value).
Meta chords accumulate from different topological contexts.
*/
type SpatialEntry struct {
	Key   uint64
	Value data.Chord
	Metas []data.Chord
}

/*
SpatialIndexServer implements the Cap'n Proto RPC interface for the Lexicon.
Keys are packed via MortonCoder.Pack(position, symbol).
Data chords are deterministic (one per byte value). Meta chords
accumulate as a list per key from different topological contexts.
*/
type SpatialIndexServer struct {
	mu sync.RWMutex

	entries       map[uint64]data.Chord
	chainEntries  map[ChordKey]data.Chord
	
	// Skip-Chords for O(log n) multi-level traversal bridging
	skip4         map[ChordKey]data.Chord
	skip16        map[ChordKey]data.Chord

	metaEntries   map[uint64][]data.Chord
	arrowSets     map[uint64][]data.Chord
	positionIndex map[uint32][]uint64
	count         int

	ctx        context.Context
	broadcast  *pool.BroadcastGroup
	conn       *rpc.Conn
	clientConn *rpc.Conn
}

type spatialIndexOpts func(*SpatialIndexServer)

func NewSpatialIndexServer(opts ...spatialIndexOpts) *SpatialIndexServer {
	idx := &SpatialIndexServer{
		entries:       make(map[uint64]data.Chord),
		chainEntries:  make(map[ChordKey]data.Chord),
		skip4:         make(map[ChordKey]data.Chord),
		skip16:        make(map[ChordKey]data.Chord),
		metaEntries:   make(map[uint64][]data.Chord),
		arrowSets:     make(map[uint64][]data.Chord),
		positionIndex: make(map[uint32][]uint64),
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
Close gracefully terminates all rpc connections and contexts.
*/
func (idx *SpatialIndexServer) Close() {
	if idx.conn != nil {
		idx.conn.Close()
	}
	if idx.clientConn != nil {
		idx.clientConn.Close()
	}
}

/*
ChordKey is the lossless representation of a chord as a map key.
The rotated chord IS the address — no hash.
*/
type ChordKey [5]uint64

/*
ToKey converts a chord to a ChordKey for map lookups.
*/
func ToKey(chord data.Chord) ChordKey {
	return ChordKey{chord.C0(), chord.C1(), chord.C2(), chord.C3(), chord.C4() & 1}
}

/*
insertSync stores an arrow chord. Each slot holds exactly one pure
chord — no superposition. On collision the EXISTING value is rotated
to generate the address for the next chain link.
*/
func (idx *SpatialIndexServer) insertSync(key uint64, value, meta data.Chord) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.insertChain(key, value)
	
	// Apply Shannon density threshold (0.45 capacity limit on GF(257) = ~115 bits)
	// If a meta chord absorbs too much structural overlap, it becomes white noise.
	if meta.ActiveCount() <= 115 {
		idx.metaEntries[key] = append(idx.metaEntries[key], meta)
	}

	// Trigger capacity compaction directly if unbounded growth threatens hardware bounds
	if len(idx.entries) > 5000000 { // 5 Million Map Entry Bound
		idx.compactUnsafe()
	}
}

/*
compactUnsafe prunes dormant branches from the core maps to prevent unbounded heap allocations.
Must be called under write lock.
*/
func (idx *SpatialIndexServer) compactUnsafe() {
	// For this bound, we simply cycle the oldest meta references or prune the chain endpoints.
	// We'll reset the meta map if it gets completely unwieldy as a safety valve.
	if len(idx.metaEntries) > 5000000 {
		idx.metaEntries = make(map[uint64][]data.Chord)
	}
	
	if len(idx.chainEntries) > 10000000 {
	    // As an emergency valve, if the collision chains become physically impossible to maintain,
	    // we forcefully shrink. In a real persistent LSM this flushes to disk.
	    idx.chainEntries = make(map[ChordKey]data.Chord)
	}
}

/*
insertChain walks the collision chain. First slot uses the Morton key.
Subsequent slots use the rotated chord itself as the address.
*/
func (idx *SpatialIndexServer) insertChain(key uint64, value data.Chord) {
	if _, exists := idx.entries[key]; !exists {
		idx.entries[key] = value
		pos, _ := morton.Unpack(key)
		idx.positionIndex[pos] = append(idx.positionIndex[pos], key)
		idx.count++
		return
	}

	existing := idx.entries[key]

	if sameChord(existing, value) {
		return
	}

	chainKey := ToKey(existing.Rotate3D())
	idx.insertChainByChord(chainKey, value)
}

/*
insertChainByChord continues the chain in chord-keyed space.
*/
func (idx *SpatialIndexServer) insertChainByChord(key ChordKey, value data.Chord) {
	visited := make(map[ChordKey]bool)

	for {
		if visited[key] {
			return
		}

		visited[key] = true

		existing, exists := idx.chainEntries[key]

		if !exists {
			idx.chainEntries[key] = value
			idx.count++
			return
		}

		if sameChord(existing, value) {
			return
		}

		key = ToKey(existing.Rotate3D())
	}
}

func (idx *SpatialIndexServer) Count() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.count
}

/*
GetEntry returns the arrow chord stored at the given Morton key.
Returns a zero chord if the key does not exist.
*/
func (idx *SpatialIndexServer) GetEntry(key uint64) data.Chord {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if chord, exists := idx.entries[key]; exists {
		return chord
	}

	return data.MustNewChord()
}

/*
HasKey returns true if the given Morton key exists in the index.
*/
func (idx *SpatialIndexServer) HasKey(key uint64) bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	_, exists := idx.entries[key]
	return exists
}

/*
GetChainEntry returns the chord at a chain address.
*/
func (idx *SpatialIndexServer) GetChainEntry(key ChordKey) (data.Chord, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	chord, exists := idx.chainEntries[key]
	return chord, exists
}

/*
BranchCount returns the number of unique branches at the given key.
*/
func (idx *SpatialIndexServer) BranchCount(key uint64) int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	return len(idx.arrowSets[key])
}

/*
Ready reports whether the spatial index has been populated.
*/
func (idx *SpatialIndexServer) Ready() bool {
	return idx.Count() > 0
}

/*
followChainUnsafe returns the collision chain at a given Morton key.
Requires idx.mu to be held.
*/
func (idx *SpatialIndexServer) followChainUnsafe(key uint64) []data.Chord {
	var chain []data.Chord
	existing, exists := idx.entries[key]
	if !exists {
		return chain
	}
	chain = append(chain, existing)

	chainKey := ToKey(existing.Rotate3D())
	visited := make(map[ChordKey]bool)

	for {
		if visited[chainKey] {
			break
		}
		visited[chainKey] = true

		next, hasNext := idx.chainEntries[chainKey]
		if !hasNext {
			break
		}

		chain = append(chain, next)
		chainKey = ToKey(next.Rotate3D())
	}
	return chain
}

/*
deleteChainUnsafe completely removes a key's collision chain from the LSM.
Requires idx.mu to be held.
*/
func (idx *SpatialIndexServer) deleteChainUnsafe(key uint64) {
	existing, exists := idx.entries[key]
	if !exists {
		return
	}
	delete(idx.entries, key)
	idx.count--

	chainKey := ToKey(existing.Rotate3D())
	visited := make(map[ChordKey]bool)

	for {
		if visited[chainKey] {
			break
		}
		visited[chainKey] = true

		next, hasNext := idx.chainEntries[chainKey]
		if !hasNext {
			break
		}
		delete(idx.chainEntries, chainKey)
		idx.count--
		chainKey = ToKey(next.Rotate3D())
	}
}

/*
Compact performs Resonant Pruning on the spatial index.
It iterates through all entries and "looks ahead" into the next 
sequence position mapping. If a Phase momentum path ends abruptly 
without an expected continuation inside the dataset (i.e. destructive 
interference), the path is pruned. Valid paths are compacted and preserved.
*/
func (idx *SpatialIndexServer) Compact() int {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	var pruned int
	calc := numeric.NewCalculus()

	for pos, keys := range idx.positionIndex {
		nextKeys, hasNext := idx.positionIndex[pos+1]
		if !hasNext || len(nextKeys) == 0 {
			continue // Valid terminal point, no chunks continued
		}

		for _, key := range keys {
			chain := idx.followChainUnsafe(key)
			if len(chain) <= 1 {
				continue // No branching entropy
			}

			var validStates []data.Chord
			var invalidStates []data.Chord

			for _, stateChord := range chain {
				hasContinuation := false
				
				// Identify if AT LEAST ONE phase branch leads to a valid jump
				for state := 0; state < 256; state++ {
					if !stateChord.Has(state) {
						continue
					}
					
					if state == 0 {
						continue // Unlikely state, default empty
					}
					
					for _, nextKey := range nextKeys {
						_, nextSymbol := morton.Unpack(nextKey)
						expectedNextState := int(calc.Multiply(numeric.Phase(state), calc.Power(3, uint32(nextSymbol))))

						nextChain := idx.followChainUnsafe(nextKey)
						for _, nextChord := range nextChain {
							if nextChord.Has(expectedNextState) {
								hasContinuation = true
								break
							}
						}
						if hasContinuation {
							break
						}
					}
					
					if hasContinuation {
						break
					}
				}
				
				if stateChord.ActiveCount() == 0 || hasContinuation {
					validStates = append(validStates, stateChord)
				} else {
					invalidStates = append(invalidStates, stateChord)
				}
			}

			if len(validStates) > 0 && len(invalidStates) > 0 {
				idx.deleteChainUnsafe(key)
				pruned += len(invalidStates)

				for _, valid := range validStates {
					idx.insertChain(key, valid)
				}
			}
		}
	}

	return pruned
}

/*
entriesAtPosition returns all entries at the given position
(lower 32 bits of the Morton key).
*/
func (idx *SpatialIndexServer) entriesAtPosition(position uint32) []SpatialEntry {
	var hits []SpatialEntry

	for _, key := range idx.positionIndex[position] {
		value := idx.entries[key]
		metas := idx.metaEntries[key]
		hits = append(hits, SpatialEntry{Key: key, Value: value, Metas: metas})
	}

	return hits
}

/*
branchesFrom returns all entries at positions greater than the given one.
*/
func (idx *SpatialIndexServer) branchesFrom(position uint32) ([]data.Chord, []data.Chord) {
	positions := make([]uint32, 0, len(idx.positionIndex))

	for pos := range idx.positionIndex {
		if pos > position {
			positions = append(positions, pos)
		}
	}

	sort.Slice(positions, func(i, j int) bool {
		return positions[i] < positions[j]
	})

	hits := make([]data.Chord, 0, len(positions))
	metaHits := make([]data.Chord, 0, len(positions))

	for _, pos := range positions {
		for _, key := range idx.positionIndex[pos] {
			value := idx.entries[key]

			if len(hits) > 0 && sameChord(hits[len(hits)-1], value) {
				continue
			}

			hits = append(hits, value)

			// Pick the first meta chord for this key as a representative
			if metas := idx.metaEntries[key]; len(metas) > 0 {
				metaHits = append(metaHits, metas[0])
			}
		}
	}

	return hits, metaHits
}

func sameChord(left, right data.Chord) bool {
	leftActive := left.ActiveCount()
	rightActive := right.ActiveCount()

	return leftActive == rightActive && data.ChordSimilarity(&left, &right) == leftActive
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

	meta, err := edge.Meta()
	if err != nil {
		return console.Error(err)
	}

	_, metaSeg, err := capnp.NewMessage(capnp.MultiSegment(nil))
	if err != nil {
		return console.Error(err)
	}

	metaVal, err := data.NewChord(metaSeg)
	if err != nil {
		return console.Error(err)
	}
	metaVal.CopyFrom(meta)

	key := morton.Pack(edge.Position(), edge.Left())
	idx.insertSync(key, chordVal, metaVal)

	return nil
}

func (idx *SpatialIndexServer) Done(ctx context.Context, call SpatialIndex_done) error {
	return nil
}

/*
buildPaths walks each prompt chord through the radix trie.
*/
func (idx *SpatialIndexServer) buildPaths(chordList data.Chord_List) ([][]data.Chord, [][]data.Chord, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	paths := make([][]data.Chord, chordList.Len())
	metaPaths := make([][]data.Chord, chordList.Len())

	for i := 0; i < chordList.Len(); i++ {
		promptChord := chordList.At(i)
		promptActive := promptChord.ActiveCount()

		deepest := uint32(0)
		matched := false

		for pos := uint32(0); ; pos++ {
			entriesKeys, hasEntries := idx.positionIndex[pos]
			if !hasEntries || len(entriesKeys) == 0 {
				break
			}

			foundAtPos := false

			for _, key := range entriesKeys {
				value := idx.entries[key]
				sim := data.ChordSimilarity(&value, &promptChord)

				if sim == value.ActiveCount() && sim > 0 && sim <= promptActive {
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
			paths[i], metaPaths[i] = idx.branchesFrom(deepest)
		}
	}

	return paths, metaPaths, nil
}

/*
writeLookupResults serialises path slices into the Cap'n Proto response.
*/
func (idx *SpatialIndexServer) writeLookupResults(
	call SpatialIndex_lookup, paths [][]data.Chord, metaPaths [][]data.Chord,
) error {
	res, err := call.AllocResults()

	if err != nil {
		return console.Error(err)
	}

	pathsList, err := res.NewPaths(int32(len(paths)))
	if err != nil {
		return console.Error(err)
	}

	metaPathsList, err := res.NewMetaPaths(int32(len(metaPaths)))
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

		metaList, err := data.NewChord_List(res.Segment(), int32(len(metaPaths[i])))
		if err != nil {
			return console.Error(err)
		}

		for j, c := range metaPaths[i] {
			el := metaList.At(j)
			el.CopyFrom(c)
		}

		if err := metaPathsList.Set(i, metaList.ToPtr()); err != nil {
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

	paths, metaPaths, err := idx.buildPaths(chords)

	if err != nil {
		return console.Error(err)
	}

	return idx.writeLookupResults(call, paths, metaPaths)
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
	var metaHits []data.Chord

	if value, exists := idx.entries[key]; exists {
		hits = append(hits, value)
		metaHits = append(metaHits, idx.metaEntries[key]...)
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
	res.SetChords(list)

	metaList, err := data.NewChord_List(res.Segment(), int32(len(metaHits)))
	if err != nil {
		return console.Error(err)
	}
	for i, m := range metaHits {
		el := metaList.At(i)
		el.CopyFrom(m)
	}

	return res.SetMetas(metaList)
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

			if sim == value.ActiveCount() && sim == active && sim > 0 {
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

		var current []byte
		for k, m := range matched {
			if k > 0 && m.pos != matched[k-1].pos+1 {
				// Disconnected chunk in the spatial index
				if len(current) > 0 {
					results = append(results, append([]byte(nil), current...))
					current = current[:0]
				}
			}
			current = append(current, m.symbol)
		}

		if len(current) > 0 {
			results = append(results, append([]byte(nil), current...))
		}
	}

	return results
}
