package store

import (
	"math"
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

	activeState geometry.IcosahedralManifold
	eigen       *geometry.EigenMode
	prevPop     int
	prevPhase   float64
	runLength   int
}

func NewPrimeField() *PrimeField {
	return &PrimeField{
		N:         0,
		manifolds: make([]geometry.IcosahedralManifold, 0),
		eigen:     geometry.NewEigenMode(),
	}
}

/*
Insert appends a chord by dynamically applying topological A_5 permutations 
to the Active Manifold based on continuous sequence derivatives (\Delta).
*/
func (field *PrimeField) Insert(chord data.Chord) {
	field.mu.Lock()
	defer field.mu.Unlock()

	// Calculate topological \Delta (Flux)
	pop := chord.ActiveCount()
	deltaPop := pop - field.prevPop
	
	// Low-entropy loop tracking
	if deltaPop >= -1 && deltaPop <= 1 {
		field.runLength++
	} else {
		field.runLength = 0
	}

	field.prevPop = pop
	
	theta, phi := field.eigen.PhaseForChord(&chord)
	phase := math.Sqrt(theta*theta + phi*phi)
	deltaPhase := math.Abs(phase - field.prevPhase)
	field.prevPhase = phase

	// Execute Pure Topological Triggers (No Linguistic Semantics)
	if pop == 0 {
		// Hard Structural Break -> Identity () + Macro_Rotate_X
		// Resets Winding, spins global RotState to demarcate boundaries
		field.activeState.Header.ResetWinding()
		currentRot := field.activeState.Header.RotState()
		field.activeState.Header.SetRotState((currentRot + 1) % 60)
		field.runLength = 0
	} else if field.runLength > 4 {
		// Low-Entropy Loop (Variance < Threshold) -> 5-Cycle Maximum Entropy Sweep
		field.activeState.Permute5Cycle(0, 1, 2, 3, 4)
	} else if deltaPop > 5 {
		// Density Spike (+Popcount) -> 3-Cycle
		field.activeState.Permute3Cycle(0, 1, 2)
	} else if deltaPhase > math.Pi/4 {
		// Phase Inversion / Orthogonal Shift -> Double Transposition
		field.activeState.PermuteDoubleTransposition(0, 3, 1, 4)
	} else if deltaPop < -5 {
		// Density Trough (-Popcount) -> 3-Cycle
		field.activeState.Permute3Cycle(0, 2, 1)
	}
	
	// Inject semantic data into local origin
	for i := 0; i < 8; i++ {
		field.activeState.Cubes[0][0][i] |= chord[i]
	}

	field.manifolds = append(field.manifolds, field.activeState)
	field.N++
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

