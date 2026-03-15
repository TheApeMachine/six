package lsm

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"

	capnp "capnproto.org/go/capnp/v3"
	"capnproto.org/go/capnp/v3/rpc"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/system/console"
	"github.com/theapemachine/six/pkg/validate"
)

var morton = data.NewMortonCoder()

/*
SpatialEntry stores a single edge in the radix forest.
Key is a MortonCoder-packed uint64: Pack(localDepth, symbol).
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
Keys are packed via MortonCoder.Pack(localDepth, symbol).
Data chords are deterministic (one per byte value). Meta chords
accumulate as a list per key from different topological contexts.
*/
type SpatialIndexServer struct {
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	serverSide   net.Conn
	clientSide   net.Conn
	client       SpatialIndex
	serverConn   *rpc.Conn
	clientConns  map[string]*rpc.Conn
	entries      map[uint64]data.Chord
	chainEntries map[ChordKey]data.Chord

	// Skip-Chords for O(log n) multi-level traversal bridging
	skip4  map[ChordKey]data.Chord
	skip16 map[ChordKey]data.Chord

	metaEntries   map[uint64][]data.Chord
	arrowSets     map[uint64][]data.Chord
	positionIndex map[uint32][]uint64
	count         int
}

type spatialIndexOpts func(*SpatialIndexServer)

func NewSpatialIndexServer(opts ...spatialIndexOpts) *SpatialIndexServer {
	idx := &SpatialIndexServer{
		clientConns:   map[string]*rpc.Conn{},
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

	validate.Require(map[string]any{
		"ctx": idx.ctx,
	})

	idx.serverSide, idx.clientSide = net.Pipe()
	idx.client = SpatialIndex_ServerToClient(idx)

	idx.serverConn = rpc.NewConn(rpc.NewStreamTransport(
		idx.serverSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(idx.client),
	})

	return idx
}

/*
Client returns a Cap'n Proto client connected to this SpatialIndexServer.
*/
func (idx *SpatialIndexServer) Client(clientID string) SpatialIndex {
	idx.clientConns[clientID] = rpc.NewConn(rpc.NewStreamTransport(
		idx.clientSide,
	), &rpc.Options{
		BootstrapClient: capnp.Client(idx.client),
	})

	return idx.client
}

/*
Close shuts down the RPC connections and underlying net.Pipe,
unblocking goroutines stuck on pipe reads.
*/
func (idx *SpatialIndexServer) Close() error {
	if idx.serverConn != nil {
		_ = idx.serverConn.Close()
		idx.serverConn = nil
	}

	for clientID, conn := range idx.clientConns {
		if conn != nil {
			_ = conn.Close()
		}
		delete(idx.clientConns, clientID)
	}

	if idx.serverSide != nil {
		_ = idx.serverSide.Close()
		idx.serverSide = nil
	}
	if idx.clientSide != nil {
		_ = idx.clientSide.Close()
		idx.clientSide = nil
	}
	if idx.cancel != nil {
		idx.cancel()
	}

	return nil
}

func (idx *SpatialIndexServer) Done(ctx context.Context, call SpatialIndex_done) error {
	return nil
}

/*
ChordKey is the lossless representation of a chord as a map key.
The rotated native value IS the collision-chain address — no hash.
*/
type ChordKey [5]uint64

/*
ToKey converts a chord to a ChordKey for map lookups.
*/
func ToKey(chord data.Chord) ChordKey {
	return ChordKey{chord.C0(), chord.C1(), chord.C2(), chord.C3(), chord.C4() & 1}
}

/*
registerArrowUnsafe records a top-level branch choice for a Morton key.
Requires idx.mu to already be held.
*/
func (idx *SpatialIndexServer) registerArrowUnsafe(key uint64, value data.Chord) {
	for _, existing := range idx.arrowSets[key] {
		if sameChord(existing, value) {
			return
		}
	}

	idx.arrowSets[key] = append(idx.arrowSets[key], value)
}

/*
insertSync stores an arrow chord. Each slot holds exactly one pure
chord — no superposition. On collision the EXISTING value is rotated
to generate the address for the next chain link.
*/
func (idx *SpatialIndexServer) insertSync(key uint64, value, meta data.Chord) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	_, symbol := morton.Unpack(key)
	storedValue := data.StorageValue(symbol, value)

	idx.insertChain(key, storedValue)
	idx.registerArrowUnsafe(key, storedValue)

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
	pos, _ := morton.Unpack(key)
	idx.positionIndex[pos] = removeUint64Key(idx.positionIndex[pos], key)
	if len(idx.positionIndex[pos]) == 0 {
		delete(idx.positionIndex, pos)
	}
	delete(idx.entries, key)
	delete(idx.arrowSets, key)
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

func removeUint64Key(keys []uint64, target uint64) []uint64 {
	if len(keys) == 0 {
		return keys
	}

	out := keys[:0]
	removed := false
	for _, key := range keys {
		if !removed && key == target {
			removed = true
			continue
		}
		out = append(out, key)
	}

	return out
}

/*
Compact performs Resonant Pruning on the spatial index.
It now follows the stored native program instead of assuming a dumb pos+1 tape:
reset returns to local depth 0, jumps advance by their encoded span, halt stays
valid, and phase prediction prefers the value's affine shell operator.
Branches that cannot find a matching continuation under that exact program are
treated as destructive interference and pruned.
*/
func (idx *SpatialIndexServer) Compact() int {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	var pruned int
	calc := numeric.NewCalculus()
	positions := make([]uint32, 0, len(idx.positionIndex))
	for pos := range idx.positionIndex {
		positions = append(positions, pos)
	}
	sort.Slice(positions, func(i, j int) bool {
		return positions[i] < positions[j]
	})

	for _, pos := range positions {
		keys := append([]uint64(nil), idx.positionIndex[pos]...)
		for _, key := range keys {
			chain := idx.followChainUnsafe(key)
			if len(chain) <= 1 {
				continue // No branching entropy
			}

			_, symbol := morton.Unpack(key)
			var validStates []data.Chord
			var invalidStates []data.Chord

			for _, stateChord := range chain {
				hasContinuation := false
				if stateChord.Terminal() || stateChord.Opcode() == uint64(data.OpcodeHalt) {
					validStates = append(validStates, stateChord)
					continue
				}

				state, ok := extractStatePhase(stateChord, symbol)
				nextPos, advances := advanceProgramPosition(pos, stateChord)
				nextKeys, hasNext := idx.positionIndex[nextPos]
				if ok && advances && hasNext {
					for _, nextKey := range nextKeys {
						_, nextSymbol := morton.Unpack(nextKey)
						expectedNextState := predictNextPhaseFromValue(calc, stateChord, state, nextSymbol)

						nextChain := idx.followChainUnsafe(nextKey)
						for _, nextChord := range nextChain {
							nextState, ok := extractStatePhase(nextChord, nextSymbol)
							if !ok {
								continue
							}
							if _, _, ok := operatorPhaseAcceptance(stateChord, expectedNextState, nextState); ok {
								hasContinuation = true
								break
							}
						}
						if hasContinuation {
							break
						}
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
					idx.registerArrowUnsafe(key, valid)
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
		_, symbol := morton.Unpack(key)
		metas := idx.metaEntries[key]
		hits = append(hits, SpatialEntry{Key: key, Value: data.ObservableValue(symbol, value), Metas: metas})
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
			_, symbol := morton.Unpack(key)
			observable := data.ObservableValue(symbol, value)

			if len(hits) > 0 && sameChord(hits[len(hits)-1], observable) {
				continue
			}

			hits = append(hits, observable)

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

/*
buildPaths reconstructs prompt bytes from the incoming observable chord list and
then runs the phase-driven traversal described in INSIGHT.md. Storage stays native
and lexical-free, while prompts and returned paths remain projected observables so
humans and higher layers can still decode them.
*/
func (idx *SpatialIndexServer) buildPaths(chordList data.Chord_List) ([][]data.Chord, [][]data.Chord, error) {
	promptBytes, err := idx.inferPromptBytes(chordList)
	if err != nil {
		return nil, nil, err
	}

	if len(promptBytes) == 0 {
		return nil, nil, nil
	}

	wf := NewWavefront(
		idx,
		WavefrontWithMaxHeads(64),
		WavefrontWithMaxDepth(uint32(len(promptBytes)+256)),
		WavefrontWithAnchors(256, 12),
	)
	interest, danger := wf.ContextSteering(string(promptBytes), "")
	results := wf.SearchPrompt(promptBytes, interest, danger)
	if len(results) == 0 {
		_, paths, metaPaths := idx.LookupByPhase(promptBytes)
		if len(paths) > 0 {
			return paths, metaPaths, nil
		}
		return nil, nil, nil
	}

	limit := len(results)
	if limit > 4 {
		limit = 4
	}

	paths := make([][]data.Chord, 0, limit)
	metaPaths := make([][]data.Chord, 0, limit)

	for i := 0; i < limit; i++ {
		paths = append(paths, results[i].Path)
		metaPaths = append(metaPaths, results[i].MetaPath)
	}

	return paths, metaPaths, nil
}

/*
inferPromptBytes extracts the raw prompt bytes from the transient query
observables produced by the tokenizer.
*/
func (idx *SpatialIndexServer) inferPromptBytes(chordList data.Chord_List) ([]byte, error) {
	prompt := make([]byte, 0, chordList.Len())

	for i := 0; i < chordList.Len(); i++ {
		chord := chordList.At(i)
		candidate, ok := inferByteFromChord(chord)
		if !ok {
			return nil, fmt.Errorf("prompt byte inference failed at index %d", i)
		}

		prompt = append(prompt, candidate)
	}

	return prompt, nil
}

/*
inferByteFromChord finds the unique lexical seed fully contained in the supplied
observable chord. It is intentionally for query/result observables, not for the
native values persisted in the spatial index.
*/
func inferByteFromChord(chord data.Chord) (byte, bool) {
	var best byte
	bestScore := -1
	unique := false

	for candidate := range 256 {
		base := data.BaseChord(byte(candidate))
		sim := data.ChordSimilarity(&base, &chord)
		if sim != base.ActiveCount() {
			continue
		}

		if sim > bestScore {
			best = byte(candidate)
			bestScore = sim
			unique = true
			continue
		}

		if sim == bestScore {
			unique = false
		}
	}

	return best, unique && bestScore > 0
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
		hits = append(hits, data.ObservableValue(byte(left), value))
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
Decode implements SpatialIndex_Server.Decode.
It accepts a list of chords from the Machine and returns byte sequences
reconstructed by the internal stateful positional decode logic.
*/
func (idx *SpatialIndexServer) Decode(
	ctx context.Context, call SpatialIndex_decode,
) error {
	chordList, err := call.Args().Chords()
	if err != nil {
		return console.Error(err)
	}

	var allSequences [][]byte
	for i := 0; i < chordList.Len(); i++ {
		ptr, err := chordList.At(i)
		if err != nil {
			return console.Error(err)
		}
		inner := data.Chord_List(ptr.List())
		slice, err := data.ChordListToSlice(inner)
		if err != nil {
			return console.Error(err)
		}

		seqs := idx.decodeChords(slice)
		allSequences = append(allSequences, seqs...)
	}

	res, err := call.AllocResults()
	if err != nil {
		return console.Error(err)
	}

	seqList, err := res.NewSequences(int32(len(allSequences)))
	if err != nil {
		return console.Error(err)
	}

	for i, seq := range allSequences {
		if err := seqList.Set(i, seq); err != nil {
			return console.Error(err)
		}
	}

	return nil
}

/*
Decode reconstructs byte sequences from result chords.
Uses positional chaining: for a path of chords, the first chord
identifies candidate (position, symbol) matches via the position
index, then each subsequent chord narrows to contiguous positions.
*/
func (idx *SpatialIndexServer) decodeChords(chords []data.Chord) [][]byte {
	if decoded, ok := idx.decodeProgrammedPath(chords); ok {
		return [][]byte{decoded}
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if len(chords) == 0 {
		return nil
	}

	type hit struct {
		pos    uint32
		symbol byte
	}

	// For each chord, find all matching (position, symbol) pairs via the position index.
	matchesByChord := make([][]hit, len(chords))

	for ci, chord := range chords {
		active := chord.ActiveCount()

		if active == 0 {
			continue
		}

		var hits []hit

		for pos, keys := range idx.positionIndex {
			for _, key := range keys {
				value := idx.entries[key]
				_, symbol := morton.Unpack(key)
				observable := data.ObservableValue(symbol, value)
				sim := data.ChordSimilarity(&observable, &chord)

				if sim == observable.ActiveCount() && sim == active && sim > 0 {
					hits = append(hits, hit{pos, symbol})
				}
			}
		}

		matchesByChord[ci] = hits
	}

	// Chain reconstruction: walk chord sequence, tracking contiguous positions.
	// Seed from the first non-empty chord's matches.
	type chain struct {
		lastPos uint32
		buf     []byte
	}

	var activeChains []chain

	for ci, hits := range matchesByChord {
		if len(hits) == 0 {
			continue
		}

		if len(activeChains) == 0 {
			// Seed: each match of the first chord starts a new chain.
			for _, h := range hits {
				activeChains = append(activeChains, chain{
					lastPos: h.pos,
					buf:     []byte{h.symbol},
				})
			}

			continue
		}

		// Extend: keep only chains where this chord matches at lastPos+1.
		hitSet := make(map[uint32]byte, len(hits))

		for _, h := range hits {
			hitSet[h.pos] = h.symbol
		}

		var extended []chain

		for _, ch := range activeChains {
			nextPos := ch.lastPos + 1

			if symbol, ok := hitSet[nextPos]; ok {
				extended = append(extended, chain{
					lastPos: nextPos,
					buf:     append(append([]byte(nil), ch.buf...), symbol),
				})
			}
		}

		if len(extended) == 0 && ci < len(matchesByChord)-1 {
			// Chain broke — emit what we have and start fresh from this chord.
			activeChains = nil

			for _, h := range hits {
				activeChains = append(activeChains, chain{
					lastPos: h.pos,
					buf:     []byte{h.symbol},
				})
			}

			continue
		}

		activeChains = extended
	}

	if len(activeChains) == 0 {
		return idx.decodeFallback(chords)
	}

	// Deduplicate: pick the longest chains and emit distinct byte sequences.
	seen := make(map[string]struct{})
	var results [][]byte

	for _, ch := range activeChains {
		s := string(ch.buf)

		if _, dup := seen[s]; dup {
			continue
		}

		seen[s] = struct{}{}
		results = append(results, append([]byte(nil), ch.buf...))
	}

	return results
}

/*
decodeProgrammedPath replays a path directly from the state chords themselves.
When traversal already returned the concrete program nodes, the lexical byte can
be inferred straight from each chord without scanning the entire spatial index.
Pure synthetic bridge chords are skipped; everything else must decode cleanly.
*/
func (idx *SpatialIndexServer) decodeProgrammedPath(chords []data.Chord) ([]byte, bool) {
	if len(chords) == 0 {
		return nil, false
	}

	out := make([]byte, 0, len(chords))
	inferred := 0

	for _, chord := range chords {
		if chord.ActiveCount() == 0 {
			continue
		}

		b, ok := inferByteFromChord(chord)
		if !ok {
			if chord.ActiveCount() <= 1 {
				continue
			}
			return nil, false
		}

		out = append(out, b)
		inferred++

		if chord.Terminal() || chord.Opcode() == uint64(data.OpcodeHalt) {
			break
		}
	}

	return out, inferred > 0
}

/*
decodeFallback handles single-chord results where positional chaining
cannot apply. Finds the shortest contiguous match per chord.
*/
func (idx *SpatialIndexServer) decodeFallback(chords []data.Chord) [][]byte {
	var results [][]byte

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

		for pos, keys := range idx.positionIndex {
			for _, key := range keys {
				value := idx.entries[key]
				_, symbol := morton.Unpack(key)
				observable := data.ObservableValue(symbol, value)
				sim := data.ChordSimilarity(&observable, &chord)

				if sim == observable.ActiveCount() && sim == active && sim > 0 {
					matched = append(matched, hit{pos, symbol})
				}
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

/*
LookupByPhase implements the exact, edit-free prompt-to-phase lookup.
It now delegates to the same reset/jump/operator-aware traversal semantics as
the wavefront, but with fuzzy edits forced to zero. Returned byte slices and
path slices are the continuation beyond the matched prompt prefix.
*/
func (idx *SpatialIndexServer) LookupByPhase(promptBytes []byte) ([][]byte, [][]data.Chord, [][]data.Chord) {
	if len(promptBytes) == 0 {
		return nil, nil, nil
	}

	wf := NewWavefront(
		idx,
		WavefrontWithMaxHeads(64),
		WavefrontWithMaxDepth(uint32(len(promptBytes)+256)),
		WavefrontWithMaxFuzzy(0),
		WavefrontWithAnchors(256, 12),
	)

	raw := wf.SearchPrompt(promptBytes, nil, nil)
	if len(raw) == 0 {
		return nil, nil, nil
	}

	limit := len(raw)
	if limit > 8 {
		limit = 8
	}

	var results [][]byte
	var resultChords [][]data.Chord
	var resultMetas [][]data.Chord
	seen := make(map[string]struct{}, limit)

	for _, candidate := range raw[:limit] {
		if len(candidate.Path) <= len(promptBytes) {
			continue
		}

		contPath := append([]data.Chord(nil), candidate.Path[len(promptBytes):]...)
		contMeta := append([]data.Chord(nil), candidate.MetaPath[len(promptBytes):]...)
		if len(contPath) == 0 {
			continue
		}

		decoded, ok := idx.decodeProgrammedPath(contPath)
		if !ok || len(decoded) == 0 {
			fallback := idx.decodeChords(contPath)
			if len(fallback) == 0 {
				continue
			}
			decoded = fallback[0]
		}

		key := string(decoded)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		results = append(results, decoded)
		resultChords = append(resultChords, contPath)
		resultMetas = append(resultMetas, contMeta)
	}

	return results, resultChords, resultMetas
}
