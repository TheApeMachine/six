package geometry

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/theapemachine/six/numeric"
)

func TestManifoldHeader(t *testing.T) {
	var header ManifoldHeader

	// Test State (1 bit)
	header.SetState(1)
	assert.Equal(t, uint8(1), header.State())

	header.SetState(0)
	assert.Equal(t, uint8(0), header.State())

	// Test RotState (6 bits: 0-63)
	header.SetRotState(42)
	assert.Equal(t, uint8(42), header.RotState())

	// Test RotState Overflow (should mask to 6 bits)
	header.SetRotState(65) // 65 & 0x3F = 1
	assert.Equal(t, uint8(1), header.RotState())

	// Test Winding Increment (modulo 16)
	for i := 0; i < 16; i++ {
		assert.Equal(t, uint8(i), header.Winding())
		header.IncrementWinding()
	}
	assert.Equal(t, uint8(0), header.Winding())

	// Test Reset Winding
	header.IncrementWinding()
	header.IncrementWinding()
	assert.Equal(t, uint8(2), header.Winding())
	header.ResetWinding()
	assert.Equal(t, uint8(0), header.Winding())

	// Test cross-contamination
	header.SetState(1)
	header.SetRotState(59)
	header.IncrementWinding()
	header.IncrementWinding()
	header.IncrementWinding()

	assert.Equal(t, uint8(1), header.State())
	assert.Equal(t, uint8(59), header.RotState())
	assert.Equal(t, uint8(3), header.Winding())
}

func TestIcosahedralManifold_LayoutSize(t *testing.T) {
	assert.Equal(t, numeric.ManifoldBytes, int(unsafe.Sizeof(IcosahedralManifold{})))
}
