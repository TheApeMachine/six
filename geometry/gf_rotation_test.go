package geometry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGFRotation_Identity(t *testing.T) {
	id := IdentityRotation()
	for face := range CubeFaces {
		require.Equal(t, face, id.Forward(face), "identity should map face to itself")
	}
}

func TestGFRotation_ForwardMatchesMicroRotateX(t *testing.T) {
	for face := 0; face < CubeFaces; face++ {
		expected := MicroRotateX[face]
		actual := RotationX.Forward(face)
		require.Equal(t, expected, actual, "RotationX.Forward(%d) should match MicroRotateX", face)
	}
}

func TestGFRotation_ForwardMatchesMicroRotateY(t *testing.T) {
	for face := range CubeFaces {
		expected := MicroRotateY[face]
		actual := RotationY.Forward(face)
		require.Equal(t, expected, actual, "RotationY.Forward(%d) should match MicroRotateY", face)
	}
}

func TestGFRotation_ForwardMatchesMicroRotateZ(t *testing.T) {
	for face := range CubeFaces {
		expected := MicroRotateZ[face]
		actual := RotationZ.Forward(face)
		require.Equal(t, expected, actual, "RotationZ.Forward(%d) should match MicroRotateZ", face)
	}
}

func TestGFRotation_CompositionMatchesSequentialPermutation(t *testing.T) {
	// Compose Y then X: should match applying MicroRotateX[MicroRotateY[face]]
	composed := RotationY.Compose(RotationX) // X ∘ Y

	for face := range CubeFaces {
		yResult := MicroRotateY[face]
		expected := MicroRotateX[yResult]
		actual := composed.Forward(face)
		require.Equal(t, expected, actual, "X(Y(%d)): composed=%d, sequential=%d", face, actual, expected)
	}
}

func TestGFRotation_InverseRoundTrips(t *testing.T) {
	rots := []GFRotation{RotationX, RotationY, RotationZ, RotationX.Compose(RotationY)}

	for _, rot := range rots {
		// Compute inverse: a⁻¹ = a^255 mod 257
		aInv := 1
		base := int(rot.A)
		for range 255 {
			aInv = (aInv * base) % CubeFaces
		}

		for face := range CubeFaces {
			phys := rot.Forward(face)
			logical := ((phys - int(rot.B) + CubeFaces) * aInv) % CubeFaces
			require.Equal(t, face, logical,
				"inverse(forward(%d)) should round-trip for rot=(%d,%d)", face, rot.A, rot.B)
		}
	}
}

func TestComposeEvents_MatchesSequentialRotation(t *testing.T) {
	events := []int{EventDensitySpike, EventPhaseInversion, EventDensityTrough}
	composed := ComposeEvents(events)

	// Apply the same events as sequential physical permutation
	for face := range CubeFaces {
		result := face
		result = MicroRotateX[result] // DensitySpike = RotateX
		result = MicroRotateY[result] // PhaseInversion = RotateY
		result = MicroRotateZ[result] // DensityTrough = RotateZ

		actual := composed.Forward(face)
		require.Equal(t, result, actual,
			"composed events at face %d: got %d, want %d", face, actual, result)
	}
}
