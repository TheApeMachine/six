package lsm

import (
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

/*
SkipLevel defines the power-of-2 stride for a skip-chord level.
Level 0 = 1 program step, Level 1 = 4 steps, Level 2 = 16 steps,
Level 3 = 64 steps.
*/
type SkipLevel int

const (
	SkipNext SkipLevel = iota
	Skip4
	Skip16
	Skip64
)

var skipStrides = [4]uint32{1, 4, 16, 64}

type skipNodeKey struct {
	key      uint64
	valueKey ChordKey
}

type skipCursor struct {
	key      uint64
	value    data.Chord
	valueKey ChordKey
	pos      uint32
	segment  uint32
	phase    numeric.Phase
}

type skipVisit struct {
	key      uint64
	valueKey ChordKey
	segment  uint32
}

/*
SkipEntry stores precomputed jump transitions for a single concrete state at a
Morton key. The concrete state is identified by ValueKey; the key itself names
the compressed radix cell. Public callers still use the root entry for a key,
while accelerated traversal follows these concrete state entries directly.
*/
type SkipEntry struct {
	Key      uint64
	ValueKey ChordKey
	Phase    numeric.Phase
	Levels   [4]SkipPhase
}

/*
SkipPhase records the landing state reached after a fixed number of exact
program transitions. The target names both the radix cell and the concrete
stored value at that cell. SegmentDelta tracks how many boundary resets were
crossed while taking the jump.
*/
type SkipPhase struct {
	Target       uint64
	TargetValue  ChordKey
	Phase        numeric.Phase
	SegmentDelta uint32
	Valid        bool
}

/*
SkipIndex augments the SpatialIndexServer with power-of-2 jump transitions for
O(log n)-style traversal. The acceleration layer is now reset/jump/operator
aware: strides follow the stored program, not a naïve pos+stride tape.
*/
type SkipIndex struct {
	idx         *SpatialIndexServer
	calc        *numeric.Calculus
	entries     map[uint64]SkipEntry
	nodeEntries map[skipNodeKey]SkipEntry
}

type skipOpts func(*SkipIndex)

/*
NewSkipIndex creates or rebuilds the skip-chord acceleration layer.
*/
func NewSkipIndex(idx *SpatialIndexServer, opts ...skipOpts) *SkipIndex {
	skip := &SkipIndex{
		idx:         idx,
		calc:        numeric.NewCalculus(),
		entries:     make(map[uint64]SkipEntry),
		nodeEntries: make(map[skipNodeKey]SkipEntry),
	}

	for _, opt := range opts {
		opt(skip)
	}

	return skip
}

/*
Build scans the spatial index and precomputes skip transitions for every
concrete stored state. The root entry for each Morton key is also exposed via
entries so existing callers can continue to jump from keys directly.
*/
func (skip *SkipIndex) Build() {
	skip.idx.mu.RLock()
	defer skip.idx.mu.RUnlock()

	skip.entries = make(map[uint64]SkipEntry, len(skip.idx.entries))
	skip.nodeEntries = make(map[skipNodeKey]SkipEntry, len(skip.idx.entries))

	for key, root := range skip.idx.entries {
		chain := skip.idx.followChainUnsafe(key)
		if len(chain) == 0 {
			chain = []data.Chord{root}
		}

		for i, value := range chain {
			entry, ok := skip.buildEntryUnsafe(key, value)
			if !ok {
				continue
			}

			node := skipNodeKey{key: key, valueKey: entry.ValueKey}
			skip.nodeEntries[node] = entry
			if i == 0 {
				skip.entries[key] = entry
			}
		}
	}
}

func (skip *SkipIndex) buildEntryUnsafe(key uint64, value data.Chord) (SkipEntry, bool) {
	pos, symbol := morton.Unpack(key)
	phase, ok := extractStatePhase(value, symbol)
	if !ok {
		return SkipEntry{}, false
	}

	cursor := skipCursor{
		key:      key,
		value:    value,
		valueKey: ToKey(value),
		pos:      pos,
		segment:  0,
		phase:    phase,
	}

	entry := SkipEntry{
		Key:      key,
		ValueKey: cursor.valueKey,
		Phase:    phase,
	}

	for level, stride := range skipStrides {
		landed, ok := skip.walkStrideUnsafe(cursor, stride)
		if !ok {
			continue
		}

		entry.Levels[level] = SkipPhase{
			Target:       landed.key,
			TargetValue:  landed.valueKey,
			Phase:        landed.phase,
			SegmentDelta: landed.segment - cursor.segment,
			Valid:        true,
		}
	}

	return entry, true
}

func (skip *SkipIndex) resolveRootEntry(key uint64) (SkipEntry, bool) {
	entry, exists := skip.entries[key]
	return entry, exists
}

func (skip *SkipIndex) resolveNodeEntry(node skipNodeKey) (SkipEntry, bool) {
	entry, exists := skip.nodeEntries[node]
	return entry, exists
}

func (skip *SkipIndex) cursorForEntryUnsafe(entry SkipEntry, segment uint32) (skipCursor, bool) {
	value, ok := skip.findValueUnsafe(entry.Key, entry.ValueKey)
	if !ok {
		return skipCursor{}, false
	}

	pos, _ := morton.Unpack(entry.Key)
	return skipCursor{
		key:      entry.Key,
		value:    value,
		valueKey: entry.ValueKey,
		pos:      pos,
		segment:  segment,
		phase:    entry.Phase,
	}, true
}

func (skip *SkipIndex) findValueUnsafe(key uint64, target ChordKey) (data.Chord, bool) {
	chain := skip.idx.followChainUnsafe(key)
	for _, value := range chain {
		if ToKey(value) == target {
			return value, true
		}
	}
	return data.Chord{}, false
}

func (skip *SkipIndex) stepCursorUnsafe(current skipCursor) (skipCursor, bool) {
	nextPos, nextSegment, ok := advanceProgramCursor(current.pos, current.segment, current.value)
	if !ok {
		return skipCursor{}, false
	}

	nextKeys, hasNext := skip.idx.positionIndex[nextPos]
	if !hasNext || len(nextKeys) == 0 {
		return skipCursor{}, false
	}

	for _, nextKey := range nextKeys {
		_, nextSymbol := morton.Unpack(nextKey)
		expectedPhase := predictNextPhaseFromValue(skip.calc, current.value, current.phase, nextSymbol)

		nextChain := skip.idx.followChainUnsafe(nextKey)
		for _, nextValue := range nextChain {
			nextPhase, ok := extractStatePhase(nextValue, nextSymbol)
			if !ok {
				continue
			}
			acceptedPhase, _, ok := operatorPhaseAcceptance(current.value, expectedPhase, nextPhase)
			if !ok {
				continue
			}

			pos, _ := morton.Unpack(nextKey)
			return skipCursor{
				key:      nextKey,
				value:    nextValue,
				valueKey: ToKey(nextValue),
				pos:      pos,
				segment:  nextSegment,
				phase:    acceptedPhase,
			}, true
		}
	}

	return skipCursor{}, false
}

func (skip *SkipIndex) walkStrideUnsafe(current skipCursor, stride uint32) (skipCursor, bool) {
	cursor := current
	for step := uint32(0); step < stride; step++ {
		next, ok := skip.stepCursorUnsafe(cursor)
		if !ok {
			return skipCursor{}, false
		}
		cursor = next
	}
	return cursor, true
}

func (skip *SkipIndex) startCursorUnsafe(startKey uint64, startPhase numeric.Phase) (skipCursor, bool) {
	chain := skip.idx.followChainUnsafe(startKey)
	pos, symbol := morton.Unpack(startKey)

	for _, value := range chain {
		if !statePhaseMatches(value, symbol, startPhase) {
			continue
		}

		return skipCursor{
			key:      startKey,
			value:    value,
			valueKey: ToKey(value),
			pos:      pos,
			segment:  0,
			phase:    startPhase,
		}, true
	}

	entry, exists := skip.entries[startKey]
	if !exists {
		return skipCursor{}, false
	}
	return skip.cursorForEntryUnsafe(entry, 0)
}

/*
Jump attempts to advance from a Morton key by the given skip level.
Returns the target key, expected phase, and whether the jump is valid.
Falls back to lower levels if the requested level is invalid.
The public API resolves against the root state at the key.
*/
func (skip *SkipIndex) Jump(key uint64, level SkipLevel) (uint64, numeric.Phase, bool) {
	entry, exists := skip.resolveRootEntry(key)
	if !exists {
		return 0, 0, false
	}

	for lvl := int(level); lvl >= 0; lvl-- {
		sp := entry.Levels[lvl]
		if sp.Valid {
			return sp.Target, sp.Phase, true
		}
	}

	return 0, 0, false
}

/*
Validate checks whether a root-state skip jump still matches the exact stored
program. The jump is re-walked through reset/jump/operator-aware traversal and
must land on the same concrete state originally recorded in the skip entry.
*/
func (skip *SkipIndex) Validate(key uint64, level SkipLevel) bool {
	skip.idx.mu.RLock()
	defer skip.idx.mu.RUnlock()

	entry, exists := skip.resolveRootEntry(key)
	if !exists {
		return false
	}
	return skip.validateEntryUnsafe(entry, level, 0)
}

func (skip *SkipIndex) validateEntryUnsafe(entry SkipEntry, level SkipLevel, segment uint32) bool {
	if int(level) < 0 || int(level) >= len(skipStrides) {
		return false
	}

	sp := entry.Levels[level]
	if !sp.Valid {
		return false
	}

	cursor, ok := skip.cursorForEntryUnsafe(entry, segment)
	if !ok {
		return false
	}

	landed, ok := skip.walkStrideUnsafe(cursor, skipStrides[level])
	if !ok {
		return false
	}

	return landed.key == sp.Target &&
		landed.valueKey == sp.TargetValue &&
		landed.phase == sp.Phase &&
		(landed.segment-cursor.segment) == sp.SegmentDelta
}

/*
SkipSearch performs an accelerated traversal using concrete skip transitions.
It starts from the concrete state whose phase matches startPhase, then prefers
validated higher-level jumps while keeping boundary-reset segment tracking. The
returned path is emitted as projected observables so callers can still decode or
measure the result in the lexical plane.
*/
func (skip *SkipIndex) SkipSearch(startKey uint64, startPhase numeric.Phase) []data.Chord {
	skip.idx.mu.RLock()
	defer skip.idx.mu.RUnlock()

	cursor, ok := skip.startCursorUnsafe(startKey, startPhase)
	if !ok {
		return nil
	}

	visited := map[skipVisit]bool{}
	path := make([]data.Chord, 0, skip.idx.count)

	for i := 0; i < int(skip.idx.count)+1; i++ {
		visit := skipVisit{key: cursor.key, valueKey: cursor.valueKey, segment: cursor.segment}
		if visited[visit] {
			break
		}
		visited[visit] = true

		_, symbol := morton.Unpack(cursor.key)
		path = append(path, data.ObservableValue(symbol, cursor.value))

		node := skipNodeKey{key: cursor.key, valueKey: cursor.valueKey}
		entry, exists := skip.resolveNodeEntry(node)
		if !exists {
			break
		}

		jumped := false
		for level := Skip64; level >= SkipNext; level-- {
			sp := entry.Levels[level]
			if !sp.Valid {
				continue
			}
			if !skip.validateEntryUnsafe(entry, level, cursor.segment) {
				continue
			}

			nextEntry, exists := skip.resolveNodeEntry(skipNodeKey{key: sp.Target, valueKey: sp.TargetValue})
			if !exists {
				continue
			}
			nextCursor, ok := skip.cursorForEntryUnsafe(nextEntry, cursor.segment+sp.SegmentDelta)
			if !ok {
				continue
			}

			nextVisit := skipVisit{key: nextCursor.key, valueKey: nextCursor.valueKey, segment: nextCursor.segment}
			if visited[nextVisit] {
				continue
			}

			cursor = nextCursor
			jumped = true
			break
		}

		if jumped {
			continue
		}

		nextCursor, ok := skip.stepCursorUnsafe(cursor)
		if !ok {
			break
		}
		cursor = nextCursor
	}

	return path
}
