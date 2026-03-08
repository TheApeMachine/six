package geometry

import (
	"sync"

	"github.com/theapemachine/six/data"
)

// Threshold constants for the dynamic topological phase transition.
const (
	MitosisThreshold   = 0.45 // The Shannon Sidestep
	DeMitosisThreshold = 0.25
	TotalBitsPerCube   = CubeFaces * 512
)

// ConditionMitosis evaluates whether "virtual mitosis" should trigger.
// Mechanically, this is a pure density threshold over the preallocated Cubes[0] array.
func (m *IcosahedralManifold) ConditionMitosis() bool {
	if m.Header.State() == 1 {
		return false // Already mitosed
	}

	activeBits := 0
	for i := range CubeFaces {
		activeBits += m.Cubes[0][i].ActiveCount()
	}

	return float64(activeBits)/float64(TotalBitsPerCube) >= MitosisThreshold
}

// ConditionDeMitosis evaluates the global density across all 5 MacroCubes.
func (m *IcosahedralManifold) ConditionDeMitosis() bool {
	if m.Header.State() == 0 {
		return false // Already cubic
	}

	activeBits := 0
	for c := range 5 {
		for i := range CubeFaces {
			activeBits += m.Cubes[c][i].ActiveCount()
		}
	}

	return float64(activeBits)/float64(TotalBitsPerCube*5) < DeMitosisThreshold
}

// Mitosis performs virtual mitosis as a state flip only.
// No memory is allocated here; Cubes[1..4] are already preallocated.
func (m *IcosahedralManifold) Mitosis() {
	if m.Header.State() == 1 {
		return
	}
	m.Header.SetState(1)
}

// DeMitosis executes the reversible structured collapse back to Cubic mode.
// Note: Geodesic Pathfinding and Fractal Pooling Cascade are handled externally
// before invoking this state flip.
func (m *IcosahedralManifold) DeMitosis() {
	if m.Header.State() == 0 {
		return
	}
	m.Header.SetState(0)
	// Clear rotation state back to cubic baseline boundaries but retain cyclic index.
	m.Header.SetRotState(m.Header.RotState() % 24)
	m.Header.ResetWinding()

	// Zero out orthogonal subspaces so the memory penalty is negated (structurally sparse)
	for c := 1; c < 5; c++ {
		for i := range CubeFaces {
			m.Cubes[c][i] = data.Chord{}
		}
	}
}

/*
GF(257) Affine Rotation Tables

Because 257 is prime, modular arithmetic mod 257 forms a perfect finite
field GF(257). The existing SO(3) rotation mechanics (RotateX/Y/Z as 3D
cyclic shifts on a 3×3×3 grid) no longer apply since 257 is prime and
cannot be factored into a 3D grid.

We replace them with three affine transformations:

	f(p) = (a·p + b) mod 257

These are non-commutative by construction:

	Y(X(p)) = 3·(p+1) = 3p + 3
	X(Y(p)) = 3p + 1
	3p + 3 ≠ 3p + 1 → sequence order is preserved.

The affine group Aff(GF(257)) has order 257 × 256 = 65,792 states,
compared to the old O group (24) or A₅ group (60).

3 is a primitive root of 257: 3^256 ≡ 1 (mod 257) and no smaller
power returns to 1. This means Y visits all 256 non-zero faces
in a single cycle.
*/
var (
	// MicroRotateX — Translation: f(p) = (p + 1) mod 257
	// A pure topological step. Every block moves one position forward.
	MicroRotateX [CubeFaces]int

	// MicroRotateY — Dilation: f(p) = (3·p) mod 257
	// 3 is a primitive root of 257. Multiplicative spin visits every
	// non-zero position exactly once per full cycle.
	MicroRotateY [CubeFaces]int

	// MicroRotateZ — Affine Combination: f(p) = (3·p + 1) mod 257
	// Combines translation and dilation. Maximum divergence trajectory.
	MicroRotateZ [CubeFaces]int

	permScratchPool = sync.Pool{
		New: func() any {
			return new(MacroCube)
		},
	}
)

func init() {
	for p := range CubeFaces {
		MicroRotateX[p] = (p + 1) % CubeFaces
		MicroRotateY[p] = (3 * p) % CubeFaces
		MicroRotateZ[p] = (3*p + 1) % CubeFaces
	}
}

// ApplyPermutation executes a CubeFaces-block structural re-indexing on a MacroCube.
// Uses a pooled scratch buffer to avoid per-call allocation without sharing
// mutable state across goroutines.
func (cube *MacroCube) ApplyPermutation(indices [CubeFaces]int) {
	scratch := permScratchPool.Get().(*MacroCube)
	for i, dst := range indices {
		scratch[dst] = cube[i]
	}
	*cube = *scratch
	permScratchPool.Put(scratch)
}

// Permute3Cycle executes an A₅ even permutation: a 3-Cycle (a -> b -> c -> a).
// Modifies the macro-topology of the 5 intersecting cubes.
func (m *IcosahedralManifold) Permute3Cycle(a, b, c int) {
	tmp := m.Cubes[c]
	m.Cubes[c] = m.Cubes[b]
	m.Cubes[b] = m.Cubes[a]
	m.Cubes[a] = tmp
}

// PermuteDoubleTransposition executes an A₅ even permutation: (a b)(c d).
// Swaps two disconnected pairs of macro-cubes simultaneously.
func (m *IcosahedralManifold) PermuteDoubleTransposition(a, b, c, d int) {
	m.Cubes[a], m.Cubes[b] = m.Cubes[b], m.Cubes[a]
	m.Cubes[c], m.Cubes[d] = m.Cubes[d], m.Cubes[c]
}

// Permute5Cycle executes a full A₅ entropy sweep: (a b c d e).
func (m *IcosahedralManifold) Permute5Cycle(a, b, c, d, e int) {
	tmp := m.Cubes[e]
	m.Cubes[e] = m.Cubes[d]
	m.Cubes[d] = m.Cubes[c]
	m.Cubes[c] = m.Cubes[b]
	m.Cubes[b] = m.Cubes[a]
	m.Cubes[a] = tmp
}

// RotateX applies a GF(257) translation to the CubeFaces blocks of the MacroCube.
func (cube *MacroCube) RotateX() {
	cube.ApplyPermutation(MicroRotateX)
}

// RotateY applies a GF(257) dilation to the CubeFaces blocks of the MacroCube.
func (cube *MacroCube) RotateY() {
	cube.ApplyPermutation(MicroRotateY)
}

// RotateZ applies a GF(257) affine combination to the CubeFaces blocks of the MacroCube.
func (cube *MacroCube) RotateZ() {
	cube.ApplyPermutation(MicroRotateZ)
}
