package lsm

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

/*
SkipLevel defines the power-of-2 stride for a skip-chord level.
Level 0 = G^1 (next), Level 1 = G^4, Level 2 = G^16, Level 3 = G^64.
*/
type SkipLevel int

const (
	SkipNext SkipLevel = iota
	Skip4
	Skip16
	Skip64
)

var skipStrides = [4]uint32{1, 4, 16, 64}

/*
SkipEntry stores precomputed jump chords at multiple power-of-2 levels
for a single Morton key. Each level stores the accumulated GF(257) phase
after applying stride rotations.
*/
type SkipEntry struct {
	Key    uint64
	Levels [4]SkipPhase
}

/*
SkipPhase pairs a target Morton key with the expected GF(257) phase
at the landing position. The target is the Morton key stride positions
ahead; the phase is (currentPhase * G^stride) mod 257.
*/
type SkipPhase struct {
	Target uint64
	Phase  numeric.Phase
	Valid  bool
}

/*
SkipIndex augments the SpatialIndexServer with power-of-2 jump chords
for O(log n) traversal. It is built lazily after initial insertion and
rebuilt on compaction.
*/
type SkipIndex struct {
	idx     *SpatialIndexServer
	calc    *numeric.Calculus
	entries map[uint64]SkipEntry
}

type skipOpts func(*SkipIndex)

/*
NewSkipIndex creates or rebuilds the skip-chord acceleration layer.
*/
func NewSkipIndex(idx *SpatialIndexServer, opts ...skipOpts) *SkipIndex {
	skip := &SkipIndex{
		idx:     idx,
		calc:    numeric.NewCalculus(),
		entries: make(map[uint64]SkipEntry),
	}

	for _, opt := range opts {
		opt(skip)
	}

	return skip
}

/*
Build scans the spatial index and precomputes skip-chords for every
Morton key. For each key at position P, it looks ahead by stride
positions (1, 4, 16, 64) and records the target key and expected
GF(257) phase transition.
*/
func (skip *SkipIndex) Build() {
	skip.idx.mu.RLock()
	defer skip.idx.mu.RUnlock()

	skip.entries = make(map[uint64]SkipEntry, len(skip.idx.entries))

	for key := range skip.idx.entries {
		pos, _ := morton.Unpack(key)

		entry := SkipEntry{Key: key}

		for level, stride := range skipStrides {
			targetPos := pos + stride
			targetKeys, hasTarget := skip.idx.positionIndex[targetPos]

			if !hasTarget || len(targetKeys) == 0 {
				continue
			}

			jumpPhase := skip.extractPhase(key)
			valid := jumpPhase != 0

			for step := uint32(1); step <= stride && valid; step++ {
				stepPos := pos + step
				stepKeys, hasStep := skip.idx.positionIndex[stepPos]

				if !hasStep || len(stepKeys) == 0 {
					valid = false
					break
				}

				_, stepSym := morton.Unpack(stepKeys[0])
				jumpPhase = skip.calc.Multiply(
					jumpPhase,
					skip.calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32(stepSym)),
				)
			}

			if !valid {
				continue
			}

			entry.Levels[level] = SkipPhase{
				Target: targetKeys[0],
				Phase:  jumpPhase,
				Valid:  true,
			}
		}

		skip.entries[key] = entry
	}
}

/*
extractPhase reads the GF(257) state from the chord stored at a key.
The stored chord is BaseChord(symbol) with one extra bit set at the
state position. XOR out the BaseChord to isolate the state bit.
*/
func (skip *SkipIndex) extractPhase(key uint64) numeric.Phase {
	chord, exists := skip.idx.entries[key]
	if !exists {
		return 0
	}

	_, symbol := morton.Unpack(key)
	base := data.BaseChord(symbol)
	stateOnly := chord.XOR(base)

	if stateOnly.ActiveCount() == 0 {
		return 0
	}

	for blockIdx := 0; blockIdx < 8; blockIdx++ {
		block := stateOnly.Block(blockIdx)
		if block == 0 {
			continue
		}
		bitIdx := bits.TrailingZeros64(block)
		primeIdx := blockIdx*64 + bitIdx
		phase := numeric.Phase(primeIdx)
		if phase >= 1 && uint32(phase) < numeric.FermatPrime {
			return phase
		}
	}

	return 0
}

/*
Jump attempts to advance from a Morton key by the given skip level.
Returns the target key, expected phase, and whether the jump is valid.
Falls back to lower levels if the requested level is invalid.
*/
func (skip *SkipIndex) Jump(key uint64, level SkipLevel) (uint64, numeric.Phase, bool) {
	entry, exists := skip.entries[key]
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
Validate checks whether a skip-chord's expected phase matches the
actual state chord at the target position. Returns true for structural
consistency.
*/
func (skip *SkipIndex) Validate(key uint64, level SkipLevel) bool {
	skip.idx.mu.RLock()
	defer skip.idx.mu.RUnlock()
	return skip.validateUnsafe(key, level)
}

func (skip *SkipIndex) validateUnsafe(key uint64, level SkipLevel) bool {
	entry, exists := skip.entries[key]
	if !exists {
		return false
	}

	sp := entry.Levels[level]
	if !sp.Valid {
		return false
	}

	targetChain := skip.idx.followChainUnsafe(sp.Target)

	for _, chord := range targetChain {
		if chord.Has(int(sp.Phase)) {
			return true
		}
	}

	return false
}

/*
SkipSearch performs an accelerated traversal using skip-chords.
Starts at the highest skip level and falls back to lower levels
when jumps fail validation, achieving O(log n) traversal.
*/
func (skip *SkipIndex) SkipSearch(
	startKey uint64, startPhase numeric.Phase,
) []data.Chord {
	skip.idx.mu.RLock()
	defer skip.idx.mu.RUnlock()

	var path []data.Chord

	currentKey := startKey
	currentPhase := startPhase

	for i := 0; i < int(skip.idx.count); i++ {
		value, exists := skip.idx.entries[currentKey]

		if !exists {
			break
		}

		path = append(path, value)

		jumped := false

		for level := Skip64; level >= SkipNext; level-- {
			targetKey, targetPhase, valid := skip.Jump(currentKey, level)

			if !valid {
				continue
			}

			if skip.validateUnsafe(currentKey, level) {
				currentKey = targetKey
				currentPhase = targetPhase
				jumped = true
				break
			}
		}

		if !jumped {
			pos, _ := morton.Unpack(currentKey)
			nextKeys, hasNext := skip.idx.positionIndex[pos+1]

			if !hasNext || len(nextKeys) == 0 {
				break
			}

			foundNext := false

			for _, nk := range nextKeys {
				_, nextSym := morton.Unpack(nk)
				expectedPhase := skip.calc.Multiply(
					currentPhase,
					skip.calc.Power(
						numeric.Phase(numeric.FermatPrimitive),
						uint32(nextSym),
					),
				)

				chain := skip.idx.followChainUnsafe(nk)

				for _, stateChord := range chain {
					if stateChord.Has(int(expectedPhase)) {
						currentKey = nk
						currentPhase = expectedPhase
						foundNext = true
						break
					}
				}

				if foundNext {
					break
				}
			}

			if !foundNext {
				break
			}
		}
	}

	return path
}
