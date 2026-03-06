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
}

/*
NewPrimeField creates a new PrimeField.
*/
func NewPrimeField() *PrimeField {
	return &PrimeField{
		N:         1,
		manifolds: make([]geometry.IcosahedralManifold, 1),
		eigen:     geometry.NewEigenMode(),
	}
}

/*
Insert appends a chord by merging it directly into the active manifold.
The stream inherently applies topological rotations passed as events.
*/
func (field *PrimeField) Insert(byteVal byte, pos uint32, chord data.Chord, events []int) {
	field.mu.Lock()
	defer field.mu.Unlock()

	for _, event := range events {
		field.applyEvent(event)
	}

	if len(events) > 0 {
		field.lastEvents = events
	}

	// 1. Thermodynamic/Entropy Routing
	// Maps chronologic time onto the intersecting Cubes to preserve Sequence.
	// Byte Value = x (cube index), Sequence Index = y (block index).
	cubeIndex := int(byteVal) % 5
	blockIndex := int(pos) % 27

	// 2. Entropy Routing (The Relief Valve)
	// If the targeted ingestion block has hit the Shannon limit,
	// mechanically rotate the cube to swing it out of the firing line
	// and expose fresh, sparse structure BEFORE inserting.
	density := float64(field.manifolds[0].Cubes[cubeIndex][blockIndex].ActiveCount()) / 512.0
	if density >= geometry.MitosisThreshold {
		field.applyEvent(geometry.EventDensitySpike)
		field.lastEvents = []int{geometry.EventDensitySpike}
	}

	// 3. Physically merge the prime chord into the specific Rubik's Cube coordinate.
	merged := data.ChordOR(&field.manifolds[0].Cubes[cubeIndex][blockIndex], &chord)
	field.manifolds[0].Cubes[cubeIndex][blockIndex] = merged
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
Rotate applies physical geometric permutations across the active manifold.
This is the heart of the non-commutative reasoning engine: structural events
viscerally spin the topological data.
*/
func (field *PrimeField) Rotate(events []int) {
	field.mu.Lock()
	defer field.mu.Unlock()

	for _, event := range events {
		field.applyEvent(event)
	}
}

func (field *PrimeField) applyEvent(event int) {
	// 1. Apply local Micro-Rotations to all 5 MacroCubes simultaneously
	for c := range 5 {
		switch event {
		case geometry.EventDensitySpike:
			field.manifolds[0].Cubes[c].RotateX()
		case geometry.EventPhaseInversion:
			field.manifolds[0].Cubes[c].RotateY()
		case geometry.EventDensityTrough:
			field.manifolds[0].Cubes[c].RotateZ()
		case geometry.EventLowVarianceFlux:
			// 180 degree rotation sweeping entropy
			field.manifolds[0].Cubes[c].RotateX()
			field.manifolds[0].Cubes[c].RotateX()
		}
	}

	// 2. Apply global A_5 tracking permutations across the 5 MacroCubes
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
