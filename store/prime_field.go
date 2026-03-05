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

func ChordPortalIndices(chord data.Chord) (int, int) {
	var h uint64
	b := chord.Bytes()
	for i := range b {
		h = (h << 5) | (h >> 59)
		h ^= uint64(b[i])
	}
	cubeIdx := int(h % 5)
	blockIdx := int((h / 5) % 27)
	return cubeIdx, blockIdx
}

type PrimeField struct {
	mu        sync.RWMutex
	manifolds []geometry.IcosahedralManifold
	refs      []GeomRef
	N         int

	activeState geometry.IcosahedralManifold
	eigen       *geometry.EigenMode
	prevPop     int
	prevPhase   float64
	runLength   int
}

// GeomRef is the exact replay address corresponding to one PrimeField index.
// It is not part of the searchable wave state.
type GeomRef struct {
	TokenID  uint64
	SampleID uint32
	Pos      uint32
	Boundary bool
}

func NewPrimeField() *PrimeField {
	return &PrimeField{
		N:         0,
		manifolds: make([]geometry.IcosahedralManifold, 0),
		refs:      make([]GeomRef, 0),
		eigen:     geometry.NewEigenMode(),
	}
}

/*
Insert appends a chord by dynamically applying topological A_5 permutations
to the Active Manifold based on continuous sequence derivatives (\Delta).
*/
func (field *PrimeField) Insert(chord data.Chord) {
	field.InsertWithRef(chord, GeomRef{})
}

/*
InsertWithRef appends a chord plus its exact replay address.
*/
func (field *PrimeField) InsertWithRef(chord data.Chord, ref GeomRef) {
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
		// Hard Structural Break -> Identity () + Micro_Rotate_X
		// Resets Winding, spins global RotState to demarcate boundaries
		field.activeState.Header.ResetWinding()
		currentRot := field.activeState.Header.RotState()
		field.activeState.Header.SetRotState((currentRot + 1) % 60)
		field.runLength = 0
		for c := range 5 {
			field.activeState.Cubes[c].RotateX()
		}
	} else if field.runLength > 4 {
		// Low-Entropy Loop (Variance < Threshold) -> 5-Cycle Maximum Entropy Sweep
		field.activeState.Permute5Cycle(0, 1, 2, 3, 4)
	} else if deltaPop > 5 {
		// Density Spike (+Popcount) -> 3-Cycle + Micro_Rotate_Y
		field.activeState.Permute3Cycle(0, 1, 2)
		for c := range 5 {
			field.activeState.Cubes[c].RotateY()
		}
	} else if deltaPhase > math.Pi/4 {
		// Phase Inversion / Orthogonal Shift -> Double Transposition + Micro_Rotate_Z
		field.activeState.PermuteDoubleTransposition(0, 3, 1, 4)
		for c := range 5 {
			field.activeState.Cubes[c].RotateZ()
		}
	} else if deltaPop < -5 {
		// Density Trough (-Popcount) -> 3-Cycle
		field.activeState.Permute3Cycle(0, 2, 1)
	}

	// Uniform hash-based multi-portal injection
	cubeIdx, blockIdx := ChordPortalIndices(chord)

	// Inject semantic data into mapped portal
	field.activeState.Cubes[cubeIdx][blockIdx] = data.ChordOR(&field.activeState.Cubes[cubeIdx][blockIdx], &chord)

	field.manifolds = append(field.manifolds, field.activeState)
	field.refs = append(field.refs, ref)
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
Ref returns the replay metadata for a given field index.
*/
func (field *PrimeField) Ref(idx int) GeomRef {
	field.mu.RLock()
	defer field.mu.RUnlock()

	return field.refs[idx]
}

/*
ReplayRefs returns the exact replay addresses from start until a boundary or limit.
*/
func (field *PrimeField) ReplayRefs(start, limit int) []GeomRef {
	refs, _ := field.ReplaySpan(start, limit)
	return refs
}

/*
ReplaySpan returns the exact replay addresses from start until a boundary or limit,
plus the inclusive end index inside the PrimeField for the replayed span.
*/
func (field *PrimeField) ReplaySpan(start, limit int) ([]GeomRef, int) {
	field.mu.RLock()
	defer field.mu.RUnlock()

	if start < 0 {
		start = 0
	}

	if limit <= 0 {
		limit = field.N - start
	}

	out := make([]GeomRef, 0, min(limit, max(field.N-start, 0)))
	endIdx := start - 1
	for idx := start; idx < field.N && len(out) < limit; idx++ {
		ref := field.refs[idx]
		if ref.Boundary {
			break
		}
		out = append(out, ref)
		endIdx = idx
	}

	return out, endIdx
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
