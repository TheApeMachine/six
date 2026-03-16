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
The data value is deterministic (5 bits per byte value).
Meta values accumulate from different topological contexts.
*/
type SpatialEntry struct {
	Key   uint64
	Value data.Value
	Metas []data.Value
}

/*
SpatialIndexServer implements the Cap'n Proto RPC interface for the Lexicon.
Keys are packed via MortonCoder.Pack(localDepth, symbol).
Data values are deterministic (one per byte value). Meta values
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
	entries      map[uint64]data.Value
	chainEntries map[ValueKey]data.Value

	// Skip-Values for O(log n) multi-level traversal bridging
	skip4  map[ValueKey]data.Value
	skip16 map[ValueKey]data.Value

	metaEntries       map[uint64][]data.Value
	arrowSets         map[uint64][]data.Value
	positionIndex     map[uint32][]uint64
	count             int
	promptWavefront   *Wavefront
	promptWavefrontMu sync.Mutex
}

type spatialIndexOpts func(*SpatialIndexServer)

func NewSpatialIndexServer(opts ...spatialIndexOpts) *SpatialIndexServer {
	idx := &SpatialIndexServer{
		clientConns:   map[string]*rpc.Conn{},
		entries:       make(map[uint64]data.Value),
		chainEntries:  make(map[ValueKey]data.Value),
		skip4:         make(map[ValueKey]data.Value),
		skip16:        make(map[ValueKey]data.Value),
		metaEntries:   make(map[uint64][]data.Value),
		arrowSets:     make(map[uint64][]data.Value),
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
ValueKey is the lossless representation of a value as a map key.
The rotated native value IS the collision-chain address — no hash.
*/
type ValueKey [5]uint64

/*
ToKey converts a value to a ValueKey for map lookups.
*/
func ToKey(value data.Value) ValueKey {
	return ValueKey{value.C0(), value.C1(), value.C2(), value.C3(), value.C4() & 1}
}

/*
registerArrowUnsafe records a top-level branch choice for a Morton key.
Requires idx.mu to already be held.
*/
func (idx *SpatialIndexServer) registerArrowUnsafe(key uint64, value data.Value) {
	for _, existing := range idx.arrowSets[key] {
		if sameValue(existing, value) {
			return
		}
	}

	idx.arrowSets[key] = append(idx.arrowSets[key], value)
}

/*
insertSync stores an arrow value. Each slot holds exactly one pure
value — no superposition. On collision the EXISTING value is rotated
to generate the address for the next chain link.
*/
func (idx *SpatialIndexServer) insertSync(key uint64, value, meta data.Value) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	_, symbol := morton.Unpack(key)
	storedValue := data.StorageValue(symbol, value)

	idx.insertChain(key, storedValue)
	idx.registerArrowUnsafe(key, storedValue)

	// Apply Shannon density threshold (0.45 capacity limit on GF(257) = ~115 bits)
	// If a meta value absorbs too much structural overlap, it becomes white noise.
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
		idx.metaEntries = make(map[uint64][]data.Value)
	}

	if len(idx.chainEntries) > 10000000 {
		// As an emergency valve, if the collision chains become physically impossible to maintain,
		// we forcefully shrink. In a real persistent LSM this flushes to disk.
		idx.chainEntries = make(map[ValueKey]data.Value)
	}
}

/*
insertChain walks the collision chain. First slot uses the Morton key.
Subsequent slots use the rotated value itself as the address.
*/
func (idx *SpatialIndexServer) insertChain(key uint64, value data.Value) {
	if _, exists := idx.entries[key]; !exists {
		idx.entries[key] = value
		pos, _ := morton.Unpack(key)
		idx.positionIndex[pos] = append(idx.positionIndex[pos], key)
		idx.count++
		return
	}

	existing := idx.entries[key]

	if sameValue(existing, value) {
		return
	}

	chainKey := ToKey(existing.Rotate3D())
	idx.insertChainByValue(chainKey, value)
}

/*
insertChainByValue continues the chain in value-keyed space.
*/
func (idx *SpatialIndexServer) insertChainByValue(key ValueKey, value data.Value) {
	visited := make(map[ValueKey]bool)

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

		if sameValue(existing, value) {
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
GetEntry returns the arrow value stored at the given Morton key.
Returns a zero value if the key does not exist.
*/
func (idx *SpatialIndexServer) GetEntry(key uint64) data.Value {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if value, exists := idx.entries[key]; exists {
		return value
	}

	return data.MustNewValue()
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
GetChainEntry returns the value at a chain address.
*/
func (idx *SpatialIndexServer) GetChainEntry(key ValueKey) (data.Value, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	value, exists := idx.chainEntries[key]
	return value, exists
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
func (idx *SpatialIndexServer) followChainUnsafe(key uint64) []data.Value {
	var chain []data.Value
	existing, exists := idx.entries[key]
	if !exists {
		return chain
	}
	chain = append(chain, existing)

	chainKey := ToKey(existing.Rotate3D())
	visited := make(map[ValueKey]bool)

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
	visited := make(map[ValueKey]bool)

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
			var validStates []data.Value
			var invalidStates []data.Value

			for _, stateValue := range chain {
				hasContinuation := false
				if stateValue.Terminal() || stateValue.Opcode() == uint64(data.OpcodeHalt) {
					validStates = append(validStates, stateValue)
					continue
				}

				state, ok := extractStatePhase(stateValue, symbol)
				nextPos, advances := advanceProgramPosition(pos, stateValue)
				nextKeys, hasNext := idx.positionIndex[nextPos]
				if ok && advances && hasNext {
					for _, nextKey := range nextKeys {
						_, nextSymbol := morton.Unpack(nextKey)
						expectedNextState := predictNextPhaseFromValue(calc, stateValue, state, nextSymbol)

						nextChain := idx.followChainUnsafe(nextKey)
						for _, nextValue := range nextChain {
							nextState, ok := extractStatePhase(nextValue, nextSymbol)
							if !ok {
								continue
							}
							if _, _, ok := operatorPhaseAcceptance(stateValue, expectedNextState, nextState); ok {
								hasContinuation = true
								break
							}
						}
						if hasContinuation {
							break
						}
					}
				}

				if stateValue.ActiveCount() == 0 || hasContinuation {
					validStates = append(validStates, stateValue)
				} else {
					invalidStates = append(invalidStates, stateValue)
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
func (idx *SpatialIndexServer) branchesFrom(position uint32) ([]data.Value, []data.Value) {
	positions := make([]uint32, 0, len(idx.positionIndex))

	for pos := range idx.positionIndex {
		if pos > position {
			positions = append(positions, pos)
		}
	}

	sort.Slice(positions, func(i, j int) bool {
		return positions[i] < positions[j]
	})

	hits := make([]data.Value, 0, len(positions))
	metaHits := make([]data.Value, 0, len(positions))

	for _, pos := range positions {
		for _, key := range idx.positionIndex[pos] {
			value := idx.entries[key]
			_, symbol := morton.Unpack(key)
			observable := data.ObservableValue(symbol, value)

			if len(hits) > 0 && sameValue(hits[len(hits)-1], observable) {
				continue
			}

			hits = append(hits, observable)

			// Pick the first meta value for this key as a representative
			if metas := idx.metaEntries[key]; len(metas) > 0 {
				metaHits = append(metaHits, metas[0])
			}
		}
	}

	return hits, metaHits
}

func sameValue(left, right data.Value) bool {
	leftActive := left.ActiveCount()
	rightActive := right.ActiveCount()

	return leftActive == rightActive && data.ValueSimilarity(&left, &right) == leftActive
}

func (idx *SpatialIndexServer) Insert(ctx context.Context, call SpatialIndex_insert) error {
	args := call.Args()
	edge, err := args.Edge()

	if err != nil {
		return console.Error(err)
	}

	value, err := edge.Value()

	if err != nil {
		return console.Error(err)
	}

	_, ownSeg, err := capnp.NewMessage(capnp.MultiSegment(nil))

	if err != nil {
		return console.Error(err)
	}

	valueVal, err := data.NewValue(ownSeg)

	if err != nil {
		return console.Error(err)
	}

	valueVal.CopyFrom(value)

	meta, err := edge.Meta()
	if err != nil {
		return console.Error(err)
	}

	_, metaSeg, err := capnp.NewMessage(capnp.MultiSegment(nil))
	if err != nil {
		return console.Error(err)
	}

	metaVal, err := data.NewValue(metaSeg)
	if err != nil {
		return console.Error(err)
	}
	metaVal.CopyFrom(meta)

	key := morton.Pack(edge.Position(), edge.Left())
	idx.insertSync(key, valueVal, metaVal)

	return nil
}

/*
buildPaths reconstructs prompt bytes from the incoming observable value list and
then runs the phase-driven traversal described in INSIGHT.md. Storage stays native
and lexical-free, while prompts and returned paths remain projected observables so
humans and higher layers can still decode them.
*/
func (idx *SpatialIndexServer) buildPaths(valueList data.Value_List) ([][]data.Value, [][]data.Value, error) {
	promptBytes, err := idx.inferPromptBytes(valueList)
	if err != nil {
		return nil, nil, err
	}

	if len(promptBytes) == 0 {
		return nil, nil, nil
	}

	interest, danger := idx.searchPromptSteering(promptBytes)
	results := idx.searchPromptWithCarry(promptBytes, interest, danger, 2, 256)
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

	paths := make([][]data.Value, 0, limit)
	metaPaths := make([][]data.Value, 0, limit)

	for i := 0; i < limit; i++ {
		paths = append(paths, results[i].Path)
		metaPaths = append(metaPaths, results[i].MetaPath)
	}

	return paths, metaPaths, nil
}

func (idx *SpatialIndexServer) searchPromptWithCarry(
	promptBytes []byte,
	interest *data.Value,
	danger *data.Value,
	maxFuzzy int,
	extraDepth uint32,
) []WavefrontResult {
	if len(promptBytes) == 0 {
		return nil
	}

	idx.promptWavefrontMu.Lock()
	defer idx.promptWavefrontMu.Unlock()

	if idx.promptWavefront == nil {
		idx.promptWavefront = NewWavefront(
			idx,
			WavefrontWithMaxHeads(64),
			WavefrontWithMaxDepth(uint32(len(promptBytes))+extraDepth),
			WavefrontWithMaxFuzzy(maxFuzzy),
			WavefrontWithAnchors(256, 12),
		)
	}

	wf := idx.promptWavefront
	wf.maxHeads = 64
	wf.maxDepth = uint32(len(promptBytes)) + extraDepth
	wf.maxFuzzy = maxFuzzy
	wf.anchorStride = 256
	wf.anchorTolerance = 12

	return wf.SearchPrompt(promptBytes, interest, danger)
}

func (idx *SpatialIndexServer) searchPromptSteering(promptBytes []byte) (*data.Value, *data.Value) {
	if len(promptBytes) == 0 {
		return nil, nil
	}

	idx.promptWavefrontMu.Lock()
	defer idx.promptWavefrontMu.Unlock()

	if idx.promptWavefront == nil {
		idx.promptWavefront = NewWavefront(
			idx,
			WavefrontWithMaxHeads(64),
			WavefrontWithMaxDepth(uint32(len(promptBytes))+256),
			WavefrontWithAnchors(256, 12),
		)
	}

	return idx.promptWavefront.ContextSteering(string(promptBytes), "")
}

/*
inferPromptBytes extracts the raw prompt bytes from the transient query
observables produced by the tokenizer.
*/
func (idx *SpatialIndexServer) inferPromptBytes(valueList data.Value_List) ([]byte, error) {
	prompt := make([]byte, 0, valueList.Len())

	for i := 0; i < valueList.Len(); i++ {
		value := valueList.At(i)
		candidate, ok := inferByteFromValue(value)
		if !ok {
			return nil, fmt.Errorf("prompt byte inference failed at index %d", i)
		}

		prompt = append(prompt, candidate)
	}

	return prompt, nil
}

/*
inferByteFromValue finds the unique lexical seed fully contained in the supplied
observable value. It is intentionally for query/result observables, not for the
native values persisted in the spatial index.
*/
func inferByteFromValue(value data.Value) (byte, bool) {
	var best byte
	bestScore := -1
	unique := false

	for candidate := range 256 {
		base := data.BaseValue(byte(candidate))
		sim := data.ValueSimilarity(&base, &value)
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
	call SpatialIndex_lookup, paths [][]data.Value, metaPaths [][]data.Value,
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
		list, err := data.NewValue_List(res.Segment(), int32(len(paths[i])))
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

		metaList, err := data.NewValue_List(res.Segment(), int32(len(metaPaths[i])))
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
	values, err := call.Args().Values()

	if err != nil {
		return console.Error(err)
	}

	paths, metaPaths, err := idx.buildPaths(values)

	if err != nil {
		return console.Error(err)
	}

	return idx.writeLookupResults(call, paths, metaPaths)
}

/*
QueryTransitions returns all chunk values stored at the given
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

	var hits []data.Value
	var metaHits []data.Value

	if value, exists := idx.entries[key]; exists {
		hits = append(hits, data.ObservableValue(byte(left), value))
		metaHits = append(metaHits, idx.metaEntries[key]...)
	}

	idx.mu.RUnlock()

	res, err := call.AllocResults()

	if err != nil {
		return console.Error(err)
	}

	list, err := data.NewValue_List(res.Segment(), int32(len(hits)))
	if err != nil {
		return console.Error(err)
	}

	for i, value := range hits {
		el := list.At(i)
		el.CopyFrom(value)
	}
	res.SetValues(list)

	metaList, err := data.NewValue_List(res.Segment(), int32(len(metaHits)))
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
It accepts a list of values from the Machine and returns byte sequences
reconstructed by the internal stateful positional decode logic.
*/
func (idx *SpatialIndexServer) Decode(
	ctx context.Context, call SpatialIndex_decode,
) error {
	valueList, err := call.Args().Values()
	if err != nil {
		return console.Error(err)
	}

	var allSequences [][]byte
	for i := 0; i < valueList.Len(); i++ {
		ptr, err := valueList.At(i)
		if err != nil {
			return console.Error(err)
		}
		inner := data.Value_List(ptr.List())
		slice, err := data.ValueListToSlice(inner)
		if err != nil {
			return console.Error(err)
		}

		seqs := idx.decodeValues(slice)
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
Decode reconstructs byte sequences from result values.
Uses positional chaining: for a path of values, the first value
identifies candidate (position, symbol) matches via the position
index, then each subsequent value narrows to contiguous positions.
*/
func (idx *SpatialIndexServer) decodeValues(values []data.Value) [][]byte {
	if decoded, ok := idx.decodeProgrammedPath(values); ok {
		return [][]byte{decoded}
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if len(values) == 0 {
		return nil
	}

	type hit struct {
		pos    uint32
		symbol byte
	}

	// For each value, find all matching (position, symbol) pairs via the position index.
	matchesByValue := make([][]hit, len(values))

	for ci, value := range values {
		active := value.ActiveCount()

		if active == 0 {
			continue
		}

		var hits []hit

		for pos, keys := range idx.positionIndex {
			for _, key := range keys {
				value := idx.entries[key]
				_, symbol := morton.Unpack(key)
				observable := data.ObservableValue(symbol, value)
				sim := data.ValueSimilarity(&observable, &value)

				if sim == observable.ActiveCount() && sim == active && sim > 0 {
					hits = append(hits, hit{pos, symbol})
				}
			}
		}

		matchesByValue[ci] = hits
	}

	// Chain reconstruction: walk value sequence, tracking contiguous positions.
	// Seed from the first non-empty value's matches.
	type chain struct {
		lastPos uint32
		buf     []byte
	}

	var activeChains []chain

	for ci, hits := range matchesByValue {
		if len(hits) == 0 {
			continue
		}

		if len(activeChains) == 0 {
			// Seed: each match of the first value starts a new chain.
			for _, h := range hits {
				activeChains = append(activeChains, chain{
					lastPos: h.pos,
					buf:     []byte{h.symbol},
				})
			}

			continue
		}

		// Extend: keep only chains where this value matches at lastPos+1.
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

		if len(extended) == 0 && ci < len(matchesByValue)-1 {
			// Chain broke — emit what we have and start fresh from this value.
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
		return idx.decodeFallback(values)
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
decodeProgrammedPath replays a path directly from the state values themselves.
When traversal already returned the concrete program nodes, the lexical byte can
be inferred straight from each value without scanning the entire spatial index.
Pure synthetic bridge values are skipped; everything else must decode cleanly.
*/
func hasAnyLexicalSeed(value data.Value) bool {
	for candidate := 0; candidate < 256; candidate++ {
		if data.HasLexicalSeed(value, byte(candidate)) {
			return true
		}
	}
	return false
}

func isSyntheticProgramValue(value data.Value) bool {
	if value.ActiveCount() == 0 {
		return true
	}
	if hasAnyLexicalSeed(value) {
		return false
	}
	return value.Opcode() != 0 ||
		value.HasAffine() ||
		value.HasTrajectory() ||
		value.HasRouteHint() ||
		value.GuardRadius() > 0 ||
		value.Mutable() ||
		value.ResidualCarry() > 0 ||
		value.OperatorFlags() != 0
}

func (idx *SpatialIndexServer) decodeProgrammedPath(values []data.Value) ([]byte, bool) {
	if len(values) == 0 {
		return nil, false
	}

	out := make([]byte, 0, len(values))
	inferred := 0

	for _, value := range values {
		if value.ActiveCount() == 0 {
			continue
		}

		b, ok := inferByteFromValue(value)
		if !ok {
			if isSyntheticProgramValue(value) {
				continue
			}
			return nil, false
		}

		out = append(out, b)
		inferred++

		if value.Terminal() || value.Opcode() == uint64(data.OpcodeHalt) {
			break
		}
	}

	if inferred == 0 {
		return nil, false
	}

	return out, true
}

/*
decodeFallback handles single-value results where positional chaining
cannot apply. Finds the shortest contiguous match per value.
*/
func (idx *SpatialIndexServer) decodeFallback(values []data.Value) [][]byte {
	var results [][]byte

	for _, value := range values {
		active := value.ActiveCount()

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
				sim := data.ValueSimilarity(&observable, &value)

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
func (idx *SpatialIndexServer) LookupByPhase(promptBytes []byte) ([][]byte, [][]data.Value, [][]data.Value) {
	if len(promptBytes) == 0 {
		return nil, nil, nil
	}

	raw := idx.searchPromptWithCarry(promptBytes, nil, nil, 0, 256)
	if len(raw) == 0 {
		return nil, nil, nil
	}

	limit := len(raw)
	if limit > 8 {
		limit = 8
	}

	var results [][]byte
	var resultValues [][]data.Value
	var resultMetas [][]data.Value
	seen := make(map[string]struct{}, limit)

	for _, candidate := range raw[:limit] {
		if len(candidate.Path) <= len(promptBytes) {
			continue
		}

		contPath := append([]data.Value(nil), candidate.Path[len(promptBytes):]...)
		contMeta := append([]data.Value(nil), candidate.MetaPath[len(promptBytes):]...)
		if len(contPath) == 0 {
			continue
		}

		decoded, ok := idx.decodeProgrammedPath(contPath)
		if !ok || len(decoded) == 0 {
			fallback := idx.decodeValues(contPath)
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
		resultValues = append(resultValues, contPath)
		resultMetas = append(resultMetas, contMeta)
	}

	return results, resultValues, resultMetas
}
