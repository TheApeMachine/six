package geometry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMitosisCondition(t *testing.T) {
	manifold := &IcosahedralManifold{}

	// Fresh manifold should not mitose
	assert.False(t, manifold.ConditionMitosis())

	// Fill [0] to exactly 45% (CubeFaces*512 * 0.45 = TotalBitsPerCube * 0.45)
	total := float64(TotalBitsPerCube)
	bitsToSet := int(total*0.45) + 1
	for i := 0; i < CubeFaces; i++ {
		for j := 0; j < 512; j++ {
			if bitsToSet > 0 {
				manifold.Cubes[0][i].Set(j)
				bitsToSet--
			}
		}
	}

	assert.True(t, manifold.ConditionMitosis())
	manifold.Mitosis()
	assert.Equal(t, uint8(1), manifold.Header.State())

	// Should not re-mitose
	assert.False(t, manifold.ConditionMitosis())
}

func TestA5Permutations(t *testing.T) {
	manifold := &IcosahedralManifold{}
	// Tag cubes for easy tracking
	manifold.Cubes[0][0].Set(0)
	manifold.Cubes[1][0].Set(1)
	manifold.Cubes[2][0].Set(2)
	manifold.Cubes[3][0].Set(3)
	manifold.Cubes[4][0].Set(4)

	// Test 3-Cycle: (0 1 2) -> 0 goes to 1, 1 goes to 2, 2 goes to 0
	manifold.Permute3Cycle(0, 1, 2)
	assert.True(t, manifold.Cubes[1][0].Has(0))
	assert.True(t, manifold.Cubes[2][0].Has(1))
	assert.True(t, manifold.Cubes[0][0].Has(2))

	// Revert
	manifold.Permute3Cycle(0, 2, 1) // Inverse 3-cycle
	assert.True(t, manifold.Cubes[0][0].Has(0))

	// Test Double Transposition: (0 3)(1 4)
	manifold.PermuteDoubleTransposition(0, 3, 1, 4)
	assert.True(t, manifold.Cubes[3][0].Has(0))
	assert.True(t, manifold.Cubes[0][0].Has(3))
	assert.True(t, manifold.Cubes[4][0].Has(1))
	assert.True(t, manifold.Cubes[1][0].Has(4))

	// Revert
	manifold.PermuteDoubleTransposition(0, 3, 1, 4)
	assert.True(t, manifold.Cubes[0][0].Has(0))

	// Test 5-Cycle: (0 1 2 3 4) -> 0 to 1, 1 to 2, 2 to 3, 3 to 4, 4 to 0
	manifold.Permute5Cycle(0, 1, 2, 3, 4)
	assert.True(t, manifold.Cubes[1][0].Has(0))
	assert.True(t, manifold.Cubes[2][0].Has(1))
	assert.True(t, manifold.Cubes[3][0].Has(2))
	assert.True(t, manifold.Cubes[4][0].Has(3))
	assert.True(t, manifold.Cubes[0][0].Has(4))
}

func TestGF257_NonCommutativity(t *testing.T) {
	// Acceptance criterion: RotateY(RotateX(p)) ≠ RotateX(RotateY(p))
	// This proves sequence order is preserved in the rotation group.
	p := 1
	xThenY := MicroRotateY[MicroRotateX[p]] // Y(X(1)) = 3*(1+1) = 6
	yThenX := MicroRotateX[MicroRotateY[p]] // X(Y(1)) = 3*1+1 = 4
	assert.NotEqual(t, xThenY, yThenX,
		"GF(257) affine rotations must be non-commutative: Y(X(%d))=%d should differ from X(Y(%d))=%d",
		p, xThenY, p, yThenX)
}

func TestGF257_PrimitiveRoot(t *testing.T) {
	// 3 is a primitive root of 257: powers of 3 (mod 257) generate all
	// 256 non-zero elements of GF(257)*.
	seen := make(map[int]bool)
	val := 1
	for i := 0; i < 256; i++ {
		val = (3 * val) % CubeFaces
		seen[val] = true
	}
	assert.Equal(t, 256, len(seen), "3 should generate all 256 non-zero elements of GF(257)")
	assert.Equal(t, 1, val, "3^256 mod 257 should be 1")
}

func TestGF257_Delimiter(t *testing.T) {
	// Face 256 is the structural delimiter — never addressed by byte values.
	// Under X (translation): (256+1) % 257 = 0 → wraps to face 0.
	// Under Y (dilation): 3*256 % 257 = 254 → maps to face 254.
	// The delimiter participates in rotations but is never data-addressed.
	assert.Equal(t, 0, MicroRotateX[CubeFaces-1],
		"X rotation of delimiter face should wrap to 0")
	assert.Equal(t, 254, MicroRotateY[CubeFaces-1],
		"Y rotation of delimiter face (3*256 mod 257 = 254)")
}
