package geometry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMitosisCondition(t *testing.T) {
	manifold := &IcosahedralManifold{}
	
	// Fresh manifold should not mitose
	assert.False(t, manifold.ConditionMitosis())

	// Fill [0] to exactly 45% (13824 * 0.45 = 6220.8 -> 6221 bits)
	bitsToSet := 6221
	for i := 0; i < 27; i++ {
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
