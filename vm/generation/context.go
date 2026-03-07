package generation

import (
	"slices"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/store"
)

type RecentSeed struct {
	Pos     uint32
	ByteVal byte // The byte value — its face index on the Fermat cube.
	Chord   data.Chord
	Events  []int
	Rot     geometry.GFRotation
}

type SlotMask struct {
	Observed [5][geometry.CubeFaces]bool
	Missing  [5][geometry.CubeFaces]bool
	Hole     [5][geometry.CubeFaces]data.Chord
	Count    int
}

func HasSeedEvent(events []int, wanted int) bool {
	return slices.Contains(events, wanted)
}

func SeedCube(events []int) int {
	switch {
	case HasSeedEvent(events, geometry.EventPhaseInversion):
		return 3
	case HasSeedEvent(events, geometry.EventDensitySpike):
		return 1
	case HasSeedEvent(events, geometry.EventLowVarianceFlux):
		return 2
	case HasSeedEvent(events, geometry.EventDensityTrough):
		return 4
	default:
		return 0
	}
}

func SupportSeedCube(events []int) int {
	cube := SeedCube(events)
	if cube == 4 {
		return 0
	}

	return cube
}

func VetoSeedCube(cube int) int {
	if cube == 4 {
		return 3
	}

	return 4
}

// SeedBlock returns the face index for a given byte value.
// This is the Fermat cube self-addressing property: the byte value
// IS the face. No hashing, no modular arithmetic, no indirection.
func SeedBlock(byteVal byte) int {
	return int(byteVal)
}

func PushRecentSeed(recent []RecentSeed, seed RecentSeed, limit int) []RecentSeed {
	if limit <= 0 {
		return recent[:0]
	}

	if len(recent) < limit {
		return append(recent, seed)
	}

	copy(recent, recent[1:])
	recent[len(recent)-1] = seed
	return recent
}

func lookupNextRotState(currentRotState uint8, ev int) (uint8, bool) {
	if ev < 0 {
		return 255, false
	}

	stateIdx := int(currentRotState)
	if stateIdx < 0 || stateIdx >= len(geometry.StateTransitionMatrix) {
		return 255, false
	}

	row := geometry.StateTransitionMatrix[stateIdx]
	if ev >= len(row) {
		return 255, false
	}

	return row[ev], true
}

// SeedQueryContext builds the query context by placing each recent seed's
// chord at its self-addressed face (byteVal). This matches PrimeField.Insert
// semantics: Insert physically rotates the cube via applyEvent, THEN places
// data at face=byteVal. The rotational state is already baked into the
// dictionary manifolds' physical layout, so we just place at byteVal.
//
// The GFRotation is tracked for header state and output inverse mapping,
// but does NOT affect where data is placed in the query context.
func SeedQueryContext(queryCtx *geometry.IcosahedralManifold, recent []RecentSeed, rot geometry.GFRotation) {
	for _, seed := range recent {
		// Apply physical layout permutations for the new events to all previously placed tokens,
		// ensuring the query context topology matches the stored PrimeField topology.
		ApplyEventsToContext(queryCtx, seed.Events)

		cubeIdx := SupportSeedCube(seed.Events)
		vetoIdx := VetoSeedCube(cubeIdx)

		// Self-addressing: byteVal IS the face, mapped through the GFRotation
		// matching the exact topological state when PrimeField was updated.
		blockIdx := seed.Rot.Forward(int(seed.ByteVal))

		current := queryCtx.Cubes[cubeIdx][blockIdx]
		veto := data.ChordHole(&current, &seed.Chord)
		merged := data.ChordOR(&current, &seed.Chord)
		queryCtx.Cubes[cubeIdx][blockIdx] = merged

		if veto.ActiveCount() > 0 {
			vetoMerged := data.ChordOR(&queryCtx.Cubes[vetoIdx][blockIdx], &veto)
			queryCtx.Cubes[vetoIdx][blockIdx] = vetoMerged
		}
	}
}

// ApplyEventsRotation composes event rotations and updates the manifold's
// header rotation state. No physical cube data is moved.
func ApplyEventsRotation(queryCtx *geometry.IcosahedralManifold, events []int) geometry.GFRotation {
	rot := geometry.IdentityRotation()

	for _, ev := range events {
		currentRotState := queryCtx.Header.RotState()
		nextRotState, ok := lookupNextRotState(currentRotState, ev)
		if !ok {
			continue
		}
		if nextRotState != 255 {
			queryCtx.Header.SetRotState(nextRotState)
		}

		rot = rot.Compose(geometry.EventRotation(ev))
	}

	return rot
}

func ApplyEventsToContext(queryCtx *geometry.IcosahedralManifold, events []int) {
	for _, ev := range events {
		currentRotState := queryCtx.Header.RotState()
		nextRotState, ok := lookupNextRotState(currentRotState, ev)
		if !ok {
			continue
		}
		if nextRotState != 255 {
			queryCtx.Header.SetRotState(nextRotState)
		}

		if queryCtx.Header.State() == 1 {
			queryCtx.Header.IncrementWinding()
		}

		switch ev {
		case geometry.EventDensitySpike:
			queryCtx.Permute3Cycle(0, 1, 2)
		case geometry.EventPhaseInversion:
			queryCtx.PermuteDoubleTransposition(0, 3, 1, 4)
		case geometry.EventDensityTrough:
			queryCtx.Permute3Cycle(0, 2, 1)
		case geometry.EventLowVarianceFlux:
			queryCtx.Permute5Cycle(0, 1, 2, 3, 4)
		}
	}
}

func MergeManifold(dst *geometry.IcosahedralManifold, src *geometry.IcosahedralManifold) {
	for c := range 5 {
		for b := range geometry.CubeFaces {
			dst.Cubes[c][b] = data.ChordOR(&dst.Cubes[c][b], &src.Cubes[c][b])
		}
	}
}

// BestFace scans all 257 faces of the given cube in the query context and
// returns the face index with the highest active bit count, plus its chord.
// The returned face index is the PHYSICAL index. To recover the logical
// byte value, pass it through the rotation's inverse (not needed for
// output since the rotation is tracked).
func BestFace(queryCtx *geometry.IcosahedralManifold, cubeIndex int) (int, data.Chord, bool) {
	bestFace := -1
	bestCount := 0
	var bestChord data.Chord

	for face := range geometry.CubeFaces {
		count := queryCtx.Cubes[cubeIndex][face].ActiveCount()
		if count > bestCount {
			bestCount = count
			bestFace = face
			bestChord = queryCtx.Cubes[cubeIndex][face]
		}
	}

	if bestFace < 0 {
		return -1, data.Chord{}, false
	}

	return bestFace, bestChord, true
}

func BestPredictedFace(priorCtx, finalCtx *geometry.IcosahedralManifold) (int, data.Chord, bool) {
	bestFace := -1
	bestCount := 0
	var bestChord data.Chord

	for c := range 4 {
		for face := range geometry.CubeFaces {
			prior := priorCtx.Cubes[c][face]
			final := finalCtx.Cubes[c][face]

			// The hole represents what was newly populated into the context.
			hole := data.ChordHole(&prior, &final)
			count := hole.ActiveCount()

			if count > bestCount {
				bestCount = count
				bestFace = face
				bestChord = hole
			}
		}
	}

	if bestFace < 0 {
		return -1, data.Chord{}, false
	}

	return bestFace, bestChord, true
}

func DeriveSlotMask(
	queryCtx *geometry.IcosahedralManifold,
	expectedReality *geometry.IcosahedralManifold,
	targetCube, targetBlock int,
) SlotMask {
	var mask SlotMask

	for c := range 5 {
		for b := range geometry.CubeFaces {
			if queryCtx.Cubes[c][b].ActiveCount() > 0 {
				mask.Observed[c][b] = true
			}
		}
	}

	for b := range geometry.CubeFaces {
		hasSupportEvidence := false
		for c := range 4 {
			if mask.Observed[c][b] {
				hasSupportEvidence = true
				break
			}
		}

		for c := range 4 {
			if mask.Observed[c][b] {
				if expectedReality != nil {
					hole := data.ChordHole(&expectedReality.Cubes[c][b], &queryCtx.Cubes[c][b])
					if hole.ActiveCount() > 0 {
						mask.Missing[c][b] = true
						mask.Hole[c][b] = hole
						mask.Count++
					}
				}
				continue
			}

			if c == targetCube && (b == targetBlock || targetBlock == -1) {
				mask.Missing[c][b] = true
				if expectedReality != nil {
					mask.Hole[c][b] = expectedReality.Cubes[c][b]
				}
				mask.Count++
				continue
			}

			if hasSupportEvidence {
				mask.Missing[c][b] = true
				mask.Count++
				continue
			}

			if expectedReality != nil && expectedReality.Cubes[c][b].ActiveCount() > 0 {
				mask.Missing[c][b] = true
				mask.Hole[c][b] = expectedReality.Cubes[c][b]
				mask.Count++
			}
		}

		if !mask.Observed[4][b] && hasSupportEvidence {
			mask.Missing[4][b] = true
			mask.Count++
		}
	}

	return mask
}

func IntegrateFill(
	queryCtx *geometry.IcosahedralManifold,
	matched *geometry.IcosahedralManifold,
	mask SlotMask,
	primefield *store.PrimeField,
) int {
	filled := 0

	for c := range 5 {
		for b := range geometry.CubeFaces {
			if !mask.Missing[c][b] {
				continue
			}

			candidate := matched.Cubes[c][b]
			if candidate.ActiveCount() == 0 {
				continue
			}

			fillChord := candidate
			if mask.Hole[c][b].ActiveCount() > 0 {
				fillChord = data.ChordGCD(&candidate, &mask.Hole[c][b])
				if fillChord.ActiveCount() == 0 {
					continue
				}
			}

			if c < 4 {
				fillChord = primefield.CleanupSnap(b, fillChord)
				prior := queryCtx.Cubes[c][b]
				veto := data.ChordHole(&prior, &fillChord)
				queryCtx.Cubes[c][b] = data.ChordOR(&prior, &fillChord)

				if veto.ActiveCount() > 0 {
					vetoCube := VetoSeedCube(c)
					queryCtx.Cubes[vetoCube][b] = data.ChordOR(&queryCtx.Cubes[vetoCube][b], &veto)
				}
			} else {
				queryCtx.Cubes[c][b] = data.ChordOR(&queryCtx.Cubes[c][b], &fillChord)
			}

			filled++
		}
	}

	return filled
}
