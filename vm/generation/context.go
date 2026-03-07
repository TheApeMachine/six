package generation

import (
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/store"
)

type RecentSeed struct {
	Pos     uint32
	ByteVal byte // The byte value — its face index on the Fermat cube.
	Chord   data.Chord
	Events  []int
}

type SlotMask struct {
	Observed [5][geometry.CubeFaces]bool
	Missing  [5][geometry.CubeFaces]bool
	Hole     [5][geometry.CubeFaces]data.Chord
	Count    int
}

func HasSeedEvent(events []int, wanted int) bool {
	for _, ev := range events {
		if ev == wanted {
			return true
		}
	}

	return false
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
	recent = append(recent, seed)
	if len(recent) <= limit {
		return recent
	}

	trimFrom := len(recent) - limit
	out := make([]RecentSeed, limit)
	copy(out, recent[trimFrom:])
	return out
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
		cubeIdx := SupportSeedCube(seed.Events)
		vetoIdx := VetoSeedCube(cubeIdx)

		// Self-addressing: byteVal IS the face, matching PrimeField.Insert.
		blockIdx := SeedBlock(seed.ByteVal)

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
		nextRotState := geometry.StateTransitionMatrix[currentRotState][ev]
		if nextRotState != 255 {
			queryCtx.Header.SetRotState(nextRotState)
		}

		rot = rot.Compose(geometry.EventRotation(ev))
	}

	return rot
}

// ApplyEventsToContext physically rotates cube data. Only used for
// PrimeField Insert paths where physical layout matters for storage.
// The generation path uses composable GFRotation instead.
func ApplyEventsToContext(queryCtx *geometry.IcosahedralManifold, events []int) {
	for _, ev := range events {
		currentRotState := queryCtx.Header.RotState()
		nextRotState := geometry.StateTransitionMatrix[currentRotState][ev]
		if nextRotState != 255 {
			queryCtx.Header.SetRotState(nextRotState)
		}

		for c := range 5 {
			switch ev {
			case geometry.EventDensitySpike:
				queryCtx.Cubes[c].RotateX()
			case geometry.EventPhaseInversion:
				queryCtx.Cubes[c].RotateY()
			case geometry.EventDensityTrough:
				queryCtx.Cubes[c].RotateZ()
			case geometry.EventLowVarianceFlux:
				queryCtx.Cubes[c].RotateX()
				queryCtx.Cubes[c].RotateX()
			}
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
func BestFace(queryCtx *geometry.IcosahedralManifold, cubeIndex int) (int, data.Chord) {
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

	return bestFace, bestChord
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
		for c := 0; c < 4; c++ {
			if mask.Observed[c][b] {
				hasSupportEvidence = true
				break
			}
		}

		for c := 0; c < 4; c++ {
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

			if c == targetCube && b == targetBlock {
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
