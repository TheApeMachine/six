package geometry

import "github.com/theapemachine/six/data"

// Threshold constants for the dynamic topological phase transition.
const (
	MitosisThreshold   = 0.45
	DeMitosisThreshold = 0.25
	TotalBitsPerCube   = 27 * 512
)

// ConditionMitosis evaluates the global density of the primary MacroCube.
func (m *IcosahedralManifold) ConditionMitosis() bool {
	if m.Header.State() == 1 {
		return false // Already mitosed
	}

	activeBits := 0
	for i := 0; i < 27; i++ {
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
	for c := 0; c < 5; c++ {
		for i := 0; i < 27; i++ {
			activeBits += m.Cubes[c][i].ActiveCount()
		}
	}

	return float64(activeBits)/float64(TotalBitsPerCube*5) < DeMitosisThreshold
}

// Mitosis triggers the 1-clock-cycle state flip and unlocks the 60 Rotational States.
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
		for i := 0; i < 27; i++ {
			m.Cubes[c][i] = data.Chord{}
		}
	}
}

// ApplyPermutation executes an exact 27-block structural re-indexing on a MacroCube.
// Used for internal Micro_Rotate_X, Y, Z logic.
func (cube *MacroCube) ApplyPermutation(indices [27]int) {
	var next MacroCube
	for i := 0; i < 27; i++ {
		next[indices[i]] = cube[i]
	}
	*cube = next
}

// Permute3Cycle executes an A_5 even permutation: a 3-Cycle (a -> b -> c -> a).
// Modifies the macro-topology of the 5 intersecting cubes.
func (m *IcosahedralManifold) Permute3Cycle(a, b, c int) {
	tmp := m.Cubes[c]
	m.Cubes[c] = m.Cubes[b]
	m.Cubes[b] = m.Cubes[a]
	m.Cubes[a] = tmp
}

// PermuteDoubleTransposition executes an A_5 even permutation: (a b)(c d).
// Swaps two disconnected pairs of macro-cubes simultaneously.
func (m *IcosahedralManifold) PermuteDoubleTransposition(a, b, c, d int) {
	m.Cubes[a], m.Cubes[b] = m.Cubes[b], m.Cubes[a]
	m.Cubes[c], m.Cubes[d] = m.Cubes[d], m.Cubes[c]
}

// Permute5Cycle executes a full A_5 entropy sweep: (a b c d e).
func (m *IcosahedralManifold) Permute5Cycle(a, b, c, d, e int) {
	tmp := m.Cubes[e]
	m.Cubes[e] = m.Cubes[d]
	m.Cubes[d] = m.Cubes[c]
	m.Cubes[c] = m.Cubes[b]
	m.Cubes[b] = m.Cubes[a]
	m.Cubes[a] = tmp
}

var (
	// Micro_Rotate_X represents a 90-degree rotation around the X-axis (Pitch).
	Micro_Rotate_X = [27]int{
		18, 19, 20, 9, 10, 11, 0, 1, 2, 21, 22, 23, 12, 13, 14, 3, 4, 5, 24, 25, 26, 15, 16, 17, 6, 7, 8,
	}

	// Micro_Rotate_Y represents a 90-degree rotation around the Y-axis (Yaw).
	Micro_Rotate_Y = [27]int{
		18, 9, 0, 21, 12, 3, 24, 15, 6, 19, 10, 1, 22, 13, 4, 25, 16, 7, 20, 11, 2, 23, 14, 5, 26, 17, 8,
	}

	// Micro_Rotate_Z represents a 90-degree rotation around the Z-axis (Roll).
	Micro_Rotate_Z = [27]int{
		6, 3, 0, 7, 4, 1, 8, 5, 2, 15, 12, 9, 16, 13, 10, 17, 14, 11, 24, 21, 18, 25, 22, 19, 26, 23, 20,
	}
)

// RotateX applies a 90-degree pitch to the 27 blocks of the MacroCube.
func (cube *MacroCube) RotateX() {
	cube.ApplyPermutation(Micro_Rotate_X)
}

// RotateY applies a 90-degree yaw to the 27 blocks of the MacroCube.
func (cube *MacroCube) RotateY() {
	cube.ApplyPermutation(Micro_Rotate_Y)
}

// RotateZ applies a 90-degree roll to the 27 blocks of the MacroCube.
func (cube *MacroCube) RotateZ() {
	cube.ApplyPermutation(Micro_Rotate_Z)
}
