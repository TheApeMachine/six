package geometry

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestGFRotation_Identity(t *testing.T) {
	Convey("Given IdentityRotation", t, func() {
		id := IdentityRotation()
		Convey("When mapping each face forward", func() {
			for face := range CubeFaces {
				So(id.Forward(face), ShouldEqual, face)
			}
		})
	})
}

func TestGFRotation_ForwardMatchesMicroRotateX(t *testing.T) {
	Convey("Given RotationX", t, func() {
		Convey("When Forward is applied to each face", func() {
			for face := 0; face < CubeFaces; face++ {
				So(DefaultRotTable.X90.Forward(face), ShouldEqual, MicroRotateX[face])
			}
		})
	})
}

func TestGFRotation_ForwardMatchesMicroRotateY(t *testing.T) {
	Convey("Given RotationY", t, func() {
		Convey("When Forward is applied to each face", func() {
			for face := range CubeFaces {
				So(DefaultRotTable.Y90.Forward(face), ShouldEqual, MicroRotateY[face])
			}
		})
	})
}

func TestGFRotation_ForwardMatchesMicroRotateZ(t *testing.T) {
	Convey("Given RotationZ", t, func() {
		Convey("When Forward is applied to each face", func() {
			for face := range CubeFaces {
				So(DefaultRotTable.Z90.Forward(face), ShouldEqual, MicroRotateZ[face])
			}
		})
	})
}

func TestGFRotation_CompositionMatchesSequentialPermutation(t *testing.T) {
	Convey("Given RotationY.Compose(RotationX)", t, func() {
		composed := DefaultRotTable.Y90.Compose(DefaultRotTable.X90)
		Convey("When Forward matches sequential MicroRotateX[MicroRotateY[face]]", func() {
			for face := range CubeFaces {
				expected := MicroRotateX[MicroRotateY[face]]
				So(composed.Forward(face), ShouldEqual, expected)
			}
		})
	})
}

func TestGFRotation_InverseRoundTrips(t *testing.T) {
	Convey("Given RotationX, Y, Z, and Y.Compose(X)", t, func() {
		rots := []GFRotation{DefaultRotTable.X90, DefaultRotTable.Y90, DefaultRotTable.Z90, DefaultRotTable.X90.Compose(DefaultRotTable.Y90)}
		Convey("When applying Forward then inverse for each face", func() {
			for _, rot := range rots {
				aInv := 1
				base := int(rot.A)
				for range 255 {
					aInv = (aInv * base) % CubeFaces
				}
				for face := range CubeFaces {
					phys := rot.Forward(face)
					logical := ((phys - int(rot.B) + CubeFaces) * aInv) % CubeFaces
					So(logical, ShouldEqual, face)
				}
			}
		})
	})
}

func TestComposeEvents_MatchesSequentialRotation(t *testing.T) {
	Convey("Given ComposeEvents(DensitySpike, PhaseInversion, DensityTrough)", t, func() {
		events := []int{EventDensitySpike, EventPhaseInversion, EventDensityTrough}
		composed := ComposeEvents(events)

		Convey("When Forward matches sequential X Y Z permutation", func() {
			for face := range CubeFaces {
				result := face
				result = MicroRotateX[result]
				result = MicroRotateY[result]
				result = MicroRotateZ[result]
				So(composed.Forward(face), ShouldEqual, result)
			}
		})
	})
}

func BenchmarkGFRotationForward(b *testing.B) {
	rot := DefaultRotTable.X90.Compose(DefaultRotTable.Y90)
	for b.Loop() {
		for face := range CubeFaces {
			_ = rot.Forward(face)
		}
	}
}

func BenchmarkGFRotationCompose(b *testing.B) {
	for b.Loop() {
		_ = DefaultRotTable.Z90.Compose(DefaultRotTable.Y90.Compose(DefaultRotTable.X90))
	}
}

func BenchmarkComposeEvents(b *testing.B) {
	events := []int{EventDensitySpike, EventPhaseInversion, EventDensityTrough, EventLowVarianceFlux}
	for b.Loop() {
		_ = ComposeEvents(events)
	}
}
