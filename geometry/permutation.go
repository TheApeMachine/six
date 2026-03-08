package geometry

import (
	"sync"

	"github.com/theapemachine/six/data"
)

/*
Threshold constants for dynamic topological phase transition.
MitosisTrigger when Cubes[0] density ≥ 45%; DeMitosis when global density < 25%.
TotalBitsPerCube = 257×512 for density normalization.
*/
const (
	MitosisThreshold   = 0.45 // Shannon Sidestep
	DeMitosisThreshold = 0.25
	TotalBitsPerCube   = CubeFaces * 512
)

/*
ConditionMitosis evaluates whether virtual mitosis should trigger.
Returns true when Cubes[0] active-bit density ≥ MitosisThreshold and not already mitosed.
*/
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

/*
ConditionDeMitosis evaluates whether to collapse back to cubic mode.
Returns true when global density across all 5 cubes < DeMitosisThreshold and already mitosed.
*/
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

/*
Mitosis performs virtual mitosis as a state flip.
Sets State=1; Cubes[1..4] are preallocated, no allocation here.
*/
func (m *IcosahedralManifold) Mitosis() {
	if m.Header.State() == 1 {
		return
	}
	m.Header.SetState(1)
}

/*
DeMitosis executes the reversible collapse back to Cubic mode.
Sets State=0, resets RotState mod 24, clears winding, zeroes Cubes[1..4].
Geodesic pathfinding and fractal pooling are handled externally beforehand.
*/
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

/*
ApplyPermutation re-indexes CubeFaces blocks according to indices.
scratch[dst] = cube[i] for each i; uses a pooled buffer to avoid allocation.
Thread-safe via sync.Pool.
*/
func (cube *MacroCube) ApplyPermutation(indices [CubeFaces]int) {
	scratch := permScratchPool.Get().(*MacroCube)
	for i, dst := range indices {
		scratch[dst] = cube[i]
	}
	*cube = *scratch
	permScratchPool.Put(scratch)
}

/*
Permute3Cycle executes the A₅ 3-cycle (a,b,c): cube a→b, b→c, c→a.
Modifies the macro-topology of the 5 intersecting cubes.
*/
func (m *IcosahedralManifold) Permute3Cycle(a, b, c int) {
	tmp := m.Cubes[c]
	m.Cubes[c] = m.Cubes[b]
	m.Cubes[b] = m.Cubes[a]
	m.Cubes[a] = tmp
}

/*
PermuteDoubleTransposition executes (a b)(c d): swaps cubes a↔b and c↔d.
A₅ even permutation; two disconnected transpositions.
*/
func (m *IcosahedralManifold) PermuteDoubleTransposition(a, b, c, d int) {
	m.Cubes[a], m.Cubes[b] = m.Cubes[b], m.Cubes[a]
	m.Cubes[c], m.Cubes[d] = m.Cubes[d], m.Cubes[c]
}

/*
Permute5Cycle executes the A₅ 5-cycle (a b c d e): a→b→c→d→e→a.
Full entropy sweep over the five macro-cubes.
*/
func (m *IcosahedralManifold) Permute5Cycle(a, b, c, d, e int) {
	tmp := m.Cubes[e]
	m.Cubes[e] = m.Cubes[d]
	m.Cubes[d] = m.Cubes[c]
	m.Cubes[c] = m.Cubes[b]
	m.Cubes[b] = m.Cubes[a]
	m.Cubes[a] = tmp
}

/*
RotateX applies GF(257) translation f(p)=(p+1) mod 257 to all faces.
*/
func (cube *MacroCube) RotateX() {
	cube.ApplyPermutation(MicroRotateX)
}

/*
RotateY applies GF(257) dilation f(p)=3p mod 257 to all faces.
*/
func (cube *MacroCube) RotateY() {
	cube.ApplyPermutation(MicroRotateY)
}

/*
RotateZ applies GF(257) affine f(p)=(3p+1) mod 257 to all faces.
*/
func (cube *MacroCube) RotateZ() {
	cube.ApplyPermutation(MicroRotateZ)
}
