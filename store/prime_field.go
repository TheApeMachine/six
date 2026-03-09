package store

import (
	"sync"
	"unsafe"

	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

/*
PrimeField is the flat, contiguous array of IcosahedralManifolds for GPU dispatch.

The LSM is cold storage. The PrimeField is the compute-side representation:
a dense 1D array of 135-block primitive manifolds that the GPU scans in parallel.
*/
type PrimeField struct {
	mu        sync.RWMutex
	manifolds []geometry.IcosahedralManifold
	N         int

	eigen      *geometry.EigenMode
	momentum   float64
	lastEvents []int
	cleanup    [4][]data.Chord
	chords     []data.Chord        // accumulated for EigenMode training
	rot        geometry.GFRotation // composable GF(257) rotation state — O(1) per event

	deMitosisHoldInserts     int
	deMitosisSparseStreak    int
	insertsSinceDensityCheck int // stride counter for expensive density scans
}

const maxCleanupPrototypesPerClass = 32

const (
	deMitosisPostMitosisHoldInserts = 2
	deMitosisSparseStreakRequired   = 3
)

func hasEvent(events []int, wanted int) bool {
	for _, event := range events {
		if event == wanted {
			return true
		}
	}

	return false
}

func cubeFromEvents(events []int) int {
	switch {
	case hasEvent(events, geometry.EventPhaseInversion):
		return 3
	case hasEvent(events, geometry.EventDensitySpike):
		return 1
	case hasEvent(events, geometry.EventLowVarianceFlux):
		return 2
	case hasEvent(events, geometry.EventDensityTrough):
		return 4
	default:
		return 0
	}
}

func supportCubeFromEvents(events []int) int {
	cube := cubeFromEvents(events)
	if cube == 4 {
		return 0
	}

	return cube
}

func vetoCubeFromSupport(cube int) int {
	if cube == 4 {
		return 3
	}

	return 4
}

func cleanupClassFromBlock(blockIdx int) int {
	if blockIdx >= 18 {
		return 3
	}

	return blockIdx % 3
}

func similarEnough(a, b *data.Chord) bool {
	aActive := a.ActiveCount()
	bActive := b.ActiveCount()
	if aActive == 0 || bActive == 0 {
		return false
	}

	minActive := aActive
	if bActive < minActive {
		minActive = bActive
	}

	sim := data.ChordSimilarity(a, b)
	return sim*2 >= minActive
}

func (field *PrimeField) rememberPrototype(blockIdx int, chord data.Chord) {
	class := cleanupClassFromBlock(blockIdx)
	bucket := field.cleanup[class]

	bestIdx := -1
	bestSim := -1
	for i := range bucket {
		sim := data.ChordSimilarity(&bucket[i], &chord)
		if sim > bestSim {
			bestSim = sim
			bestIdx = i
		}
	}

	if bestIdx >= 0 && similarEnough(&bucket[bestIdx], &chord) {
		shared := data.ChordAND(&bucket[bestIdx], &chord)
		if shared.ActiveCount() > 0 {
			bucket[bestIdx] = shared
		} else {
			bucket[bestIdx] = chord
		}
		field.cleanup[class] = bucket
		return
	}

	if len(bucket) < maxCleanupPrototypesPerClass {
		field.cleanup[class] = append(bucket, chord)
		return
	}

	worstIdx := 0
	worstSim := data.ChordSimilarity(&bucket[0], &chord)
	for i := 1; i < len(bucket); i++ {
		sim := data.ChordSimilarity(&bucket[i], &chord)
		if sim < worstSim {
			worstSim = sim
			worstIdx = i
		}
	}

	bucket[worstIdx] = chord
	field.cleanup[class] = bucket
}

func (field *PrimeField) snapPrototype(blockIdx int, chord data.Chord) data.Chord {
	class := cleanupClassFromBlock(blockIdx)
	bucket := field.cleanup[class]
	if len(bucket) == 0 {
		return chord
	}

	bestIdx := 0
	bestSim := data.ChordSimilarity(&bucket[0], &chord)
	for i := 1; i < len(bucket); i++ {
		sim := data.ChordSimilarity(&bucket[i], &chord)
		if sim > bestSim {
			bestSim = sim
			bestIdx = i
		}
	}

	a := chord.ActiveCount()
	if a == 0 {
		return chord
	}

	if bestSim*4 >= a*3 {
		return bucket[bestIdx]
	}

	return chord
}

func shouldOverwriteSupport(current, incoming *data.Chord) bool {
	if current.ActiveCount() == 0 {
		return true
	}

	shared := data.ChordSimilarity(current, incoming)
	staleChord := data.ChordHole(current, incoming)
	freshChord := data.ChordHole(incoming, current)
	stale := staleChord.ActiveCount()
	fresh := freshChord.ActiveCount()

	// If the new observation introduces substantial new structure and most
	// of the old support is now stale, bias toward replacement instead of
	// monotonic OR accumulation.
	return fresh > 0 && stale > shared
}

/*
NewPrimeField creates a new PrimeField.
*/
func NewPrimeField() *PrimeField {
	return &PrimeField{
		N:         1,
		manifolds: make([]geometry.IcosahedralManifold, 1),
		eigen:     geometry.NewEigenMode(),
		rot:       geometry.IdentityRotation(),
	}
}

/*
StorePointer ORs ptr into face 256 at the slot given by rot.Forward(256).
Face 256 is the structural delimiter; used for sequence pointers.
*/
func (field *PrimeField) StorePointer(rot geometry.GFRotation, ptr data.Chord) {
	slot := rot.Forward(256)

	field.mu.Lock()
	defer field.mu.Unlock()

	stored := field.manifolds[0].Cubes[0][slot]
	field.manifolds[0].Cubes[0][slot] = data.ChordOR(&stored, &ptr)
}

/*
GetPointer returns the chord stored at face 256 for the given rotation state.
Collisions are OR-merged during StorePointer.
*/
func (field *PrimeField) GetPointer(rot geometry.GFRotation) data.Chord {
	slot := rot.Forward(256)

	field.mu.RLock()
	defer field.mu.RUnlock()

	return field.manifolds[0].Cubes[0][slot]
}

/*
ManifoldsPtr provides the raw memory pointer to the start of the contiguous
IcosahedralManifold array, used for direct GPU dispatch without copying.
*/
func (field *PrimeField) ManifoldsPtr() unsafe.Pointer {
	field.mu.RLock()
	defer field.mu.RUnlock()
	return unsafe.Pointer(&field.manifolds[0])
}

func (field *PrimeField) activeHasSignal() bool {
	for cubeIdx := range 5 {
		for blockIdx := range geometry.CubeFaces {
			if field.manifolds[0].Cubes[cubeIdx][blockIdx].ActiveCount() > 0 {
				return true
			}
		}
	}

	return false
}

/*
BuildEigenModes passes accumulated chords to EigenMode.BuildMultiScaleCooccurrence.
Call after ingestion; clears field.chords. EigenMode is analytical (no-op build).
*/
func (field *PrimeField) BuildEigenModes() error {
	field.mu.RLock()
	chords := make([]data.Chord, len(field.chords))
	copy(chords, field.chords)
	field.mu.RUnlock()

	if len(chords) == 0 {
		field.mu.Lock()
		field.chords = nil
		field.mu.Unlock()
		return nil
	}

	if err := field.eigen.BuildMultiScaleCooccurrence(chords); err != nil {
		return err
	}

	field.mu.Lock()
	field.chords = nil
	field.mu.Unlock()

	return nil
}

/*
EigenMode returns the PrimeField's EigenMode for phase lookups (Theta, Phi).
*/
func (field *PrimeField) EigenMode() *geometry.EigenMode {
	field.mu.RLock()
	defer field.mu.RUnlock()

	return field.eigen
}

func (field *PrimeField) freezeActiveIfBoundary(pos uint32) {
	if pos != 0 {
		return
	}

	if !field.activeHasSignal() {
		return
	}

	field.manifolds = append(field.manifolds, field.manifolds[0])
	field.manifolds[0] = geometry.IcosahedralManifold{}
	field.N = len(field.manifolds)

	// Reset rotation state for the fresh active manifold.
	// The frozen manifold's data layout already reflects its rotation history.
	field.rot = geometry.IdentityRotation()
}

func (field *PrimeField) cubeDensity(cubeIdx int) float64 {
	activeBits := 0
	for blockIdx := range geometry.CubeFaces {
		activeBits += field.manifolds[0].Cubes[cubeIdx][blockIdx].ActiveCount()
	}

	return float64(activeBits) / float64(geometry.TotalBitsPerCube)
}

func (field *PrimeField) shouldDeMitosis() bool {
	active := &field.manifolds[0]
	if active.Header.State() == 0 {
		field.deMitosisHoldInserts = 0
		field.deMitosisSparseStreak = 0
		return false
	}

	if active.Header.Winding() == 0 {
		field.deMitosisSparseStreak = 0
		return false
	}

	if field.deMitosisHoldInserts > 0 {
		field.deMitosisHoldInserts--
		field.deMitosisSparseStreak = 0
		return false
	}

	if !active.ConditionDeMitosis() {
		field.deMitosisSparseStreak = 0
		return false
	}

	if field.cubeDensity(0) >= geometry.DeMitosisThreshold {
		field.deMitosisSparseStreak = 0
		return false
	}

	field.deMitosisSparseStreak++
	if field.deMitosisSparseStreak < deMitosisSparseStreakRequired {
		return false
	}

	field.deMitosisSparseStreak = 0

	return true
}

/*
Insert stores a chord on the cube as a radix trie node.

The chord's IntrinsicFace determines the logical face (0-255). The accumulated
rotation state determines the slot: blockIndex = rot.Forward(logicalFace).
After writing, RotationForChord is composed into the running rotation,
matching the composition order during recall.

All data goes to cube 0 — the rotation IS the addressing mechanism.
*/
func (field *PrimeField) Insert(chord data.Chord) {
	field.mu.Lock()
	defer field.mu.Unlock()

	field.chords = append(field.chords, chord)

	// Compute the block index from the current rotation state.
	logicalFace := chord.IntrinsicFace()
	blockIndex := field.rot.Forward(logicalFace)

	// Write the chord to cube 0 at the rotation-addressed slot.
	// Collision = compression: if something is already there, OR-merge.
	current := field.manifolds[0].Cubes[0][blockIndex]
	if current.ActiveCount() == 0 {
		field.manifolds[0].Cubes[0][blockIndex] = chord
	} else {
		merged := data.ChordOR(&current, &chord)
		field.manifolds[0].Cubes[0][blockIndex] = merged
	}

	// Compose RotationForChord AFTER storing — this advances the rotation
	// cursor to the next position.
	field.rot = field.rot.Compose(geometry.RotationForChord(chord))
}

/*
SetMomentum updates the current rotational velocity coefficient (DeltaPhase).
*/
func (field *PrimeField) SetMomentum(momentum float64) {
	field.mu.Lock()
	defer field.mu.Unlock()
	field.momentum = momentum
}

/*
Momentum returns the current rotational velocity coefficient and the
last driving sequence of topological events.
*/
func (field *PrimeField) Momentum() (float64, []int) {
	field.mu.RLock()
	defer field.mu.RUnlock()
	return field.momentum, field.lastEvents
}

/*
Rotate applies each event's A₅ permutation to the 5 cubes and composes
EventRotation into field.rot. Updates RotState via StateTransitionMatrix.
*/
func (field *PrimeField) Rotate(events []int) {
	field.mu.Lock()
	defer field.mu.Unlock()

	for _, event := range events {
		field.applyEvent(event)
	}
}

func (field *PrimeField) applyEvent(event int) {
	state := int(field.manifolds[0].Header.RotState())
	if state >= 0 && state < len(geometry.StateTransitionMatrix) {
		next := geometry.StateTransitionMatrix[state][event]
		if next != 255 {
			field.manifolds[0].Header.SetRotState(next)
		}
	}

	if field.manifolds[0].Header.State() == 1 {
		field.manifolds[0].Header.IncrementWinding()
	}

	// Compose the GF(257) micro-rotation into the running state.
	// O(1) arithmetic replaces O(257 × 5 × 64) bytes of data movement.
	field.rot = field.rot.Compose(geometry.EventRotation(event))

	// A₅ macro-permutations across the 5 cubes — these are cheap
	// struct swaps, not per-face data copies.
	switch event {
	case geometry.EventDensitySpike:
		field.manifolds[0].Permute3Cycle(0, 1, 2)
	case geometry.EventPhaseInversion:
		field.manifolds[0].PermuteDoubleTransposition(0, 3, 1, 4)
	case geometry.EventDensityTrough:
		field.manifolds[0].Permute3Cycle(0, 2, 1)
	case geometry.EventLowVarianceFlux:
		field.manifolds[0].Permute5Cycle(0, 1, 2, 3, 4)
	}
}

/*
Snapshot returns an atomic snapshot of the memory array pointer and its exact bounds (N)
to prevent concurrency tearing where N might exceed the bounds of the returned backing array.
*/
func (field *PrimeField) Snapshot() (unsafe.Pointer, int) {
	field.mu.RLock()
	defer field.mu.RUnlock()

	if field.N == 0 {
		return nil, 0
	}

	return unsafe.Pointer(&field.manifolds[0]), field.N
}

/*
SearchSnapshot returns a pointer to the frozen manifold array (excluding active manifold[0]),
the count of frozen manifolds, and the start offset. For single-manifold fields returns (manifold[0], 1, 0).
*/
func (field *PrimeField) SearchSnapshot() (unsafe.Pointer, int, int) {
	field.mu.RLock()
	defer field.mu.RUnlock()

	if field.N == 0 || len(field.manifolds) == 0 {
		return nil, 0, 0
	}

	if len(field.manifolds) == 1 {
		return unsafe.Pointer(&field.manifolds[0]), 1, 0
	}

	return unsafe.Pointer(&field.manifolds[1]), len(field.manifolds) - 1, 1
}

/*
CleanupSnap returns the best-matching prototype from the cleanup bucket for blockIdx,
or the chord unchanged if no prototype meets the similarity threshold (≥75% overlap).
*/
func (field *PrimeField) CleanupSnap(blockIdx int, chord data.Chord) data.Chord {
	field.mu.RLock()
	defer field.mu.RUnlock()

	return field.snapPrototype(blockIdx, chord)
}

/*
Field returns a pointer to the contiguous manifold array for GPU dispatch.
The caller must hold the data stable for the duration of the GPU call.
*/
func (field *PrimeField) Field() unsafe.Pointer {
	field.mu.RLock()
	defer field.mu.RUnlock()

	if field.N == 0 {
		return nil
	}

	return unsafe.Pointer(&field.manifolds[0])
}

/*
Manifold returns the raw IcosahedralManifold at a given index.
*/
func (field *PrimeField) Manifold(idx int) geometry.IcosahedralManifold {
	field.mu.RLock()
	defer field.mu.RUnlock()

	return field.manifolds[idx]
}

/*
Mask temporarily zeros out a manifold to exclude it from BestFill searches.
It returns the original structure so it can be unmasked later.
*/
func (field *PrimeField) Mask(idx int) geometry.IcosahedralManifold {
	field.mu.Lock()
	defer field.mu.Unlock()

	original := field.manifolds[idx]
	field.manifolds[idx] = geometry.IcosahedralManifold{}
	return original
}

/*
Unmask restores a previously masked manifold.
*/
func (field *PrimeField) Unmask(idx int, manifold geometry.IcosahedralManifold) {
	field.mu.Lock()
	defer field.mu.Unlock()

	field.manifolds[idx] = manifold
}
