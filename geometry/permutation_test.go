package geometry

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestMitosisCondition(t *testing.T) {
	Convey("Given a fresh IcosahedralManifold", t, func() {
		manifold := &IcosahedralManifold{}

		Convey("When density is zero", func() {
			So(manifold.ConditionMitosis(), ShouldBeFalse)
		})

		Convey("When Cubes[0] reaches 45% density", func() {
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
			So(manifold.ConditionMitosis(), ShouldBeTrue)
			manifold.Mitosis()
			So(manifold.Header.State(), ShouldEqual, uint8(1))
		})

		Convey("When already mitosed", func() {
			manifold.Header.SetState(1)
			So(manifold.ConditionMitosis(), ShouldBeFalse)
		})
	})
}

func TestA5Permutations(t *testing.T) {
	Convey("Given an IcosahedralManifold with tagged cubes", t, func() {
		manifold := &IcosahedralManifold{}
		manifold.Cubes[0][0].Set(0)
		manifold.Cubes[1][0].Set(1)
		manifold.Cubes[2][0].Set(2)
		manifold.Cubes[3][0].Set(3)
		manifold.Cubes[4][0].Set(4)

		Convey("When Permute3Cycle(0,1,2) is applied", func() {
			manifold.Permute3Cycle(0, 1, 2)
			So(manifold.Cubes[1][0].Has(0), ShouldBeTrue)
			So(manifold.Cubes[2][0].Has(1), ShouldBeTrue)
			So(manifold.Cubes[0][0].Has(2), ShouldBeTrue)
		})

		Convey("When inverse 3-cycle reverts", func() {
			manifold.Permute3Cycle(0, 1, 2)
			manifold.Permute3Cycle(0, 2, 1)
			So(manifold.Cubes[0][0].Has(0), ShouldBeTrue)
		})

		Convey("When PermuteDoubleTransposition(0,3)(1,4) is applied", func() {
			manifold.PermuteDoubleTransposition(0, 3, 1, 4)
			So(manifold.Cubes[3][0].Has(0), ShouldBeTrue)
			So(manifold.Cubes[0][0].Has(3), ShouldBeTrue)
			So(manifold.Cubes[4][0].Has(1), ShouldBeTrue)
			So(manifold.Cubes[1][0].Has(4), ShouldBeTrue)
		})

		Convey("When double transposition reverts", func() {
			manifold.PermuteDoubleTransposition(0, 3, 1, 4)
			manifold.PermuteDoubleTransposition(0, 3, 1, 4)
			So(manifold.Cubes[0][0].Has(0), ShouldBeTrue)
		})

		Convey("When Permute5Cycle(0,1,2,3,4) is applied", func() {
			manifold.Permute5Cycle(0, 1, 2, 3, 4)
			So(manifold.Cubes[1][0].Has(0), ShouldBeTrue)
			So(manifold.Cubes[2][0].Has(1), ShouldBeTrue)
			So(manifold.Cubes[3][0].Has(2), ShouldBeTrue)
			So(manifold.Cubes[4][0].Has(3), ShouldBeTrue)
			So(manifold.Cubes[0][0].Has(4), ShouldBeTrue)
		})
	})
}

func TestGF257_NonCommutativity(t *testing.T) {
	Convey("Given GF(257) affine rotations", t, func() {
		Convey("When Y(X(p)) and X(Y(p)) are compared", func() {
			p := 1
			xThenY := MicroRotateY[MicroRotateX[p]]
			yThenX := MicroRotateX[MicroRotateY[p]]
			So(xThenY, ShouldNotEqual, yThenX)
		})
	})
}

func TestGF257_PrimitiveRoot(t *testing.T) {
	Convey("Given 3 as generator of GF(257)*", t, func() {
		Convey("When iterating 3^k for k=1..256", func() {
			seen := make(map[int]bool)
			val := 1
			for i := 0; i < 256; i++ {
				val = (3 * val) % CubeFaces
				seen[val] = true
			}
			So(len(seen), ShouldEqual, 256)
			So(val, ShouldEqual, 1)
		})
	})
}

func TestGF257_Delimiter(t *testing.T) {
	Convey("Given face 256 (structural delimiter)", t, func() {
		Convey("When RotationX is applied", func() {
			So(MicroRotateX[CubeFaces-1], ShouldEqual, 0)
		})
		Convey("When RotationY is applied", func() {
			So(MicroRotateY[CubeFaces-1], ShouldEqual, 254)
		})
	})
}

func BenchmarkMacroCubeRotateX(b *testing.B) {
	var cube MacroCube
	cube[0].Set(0)
	cube[1].Set(1)
	b.ResetTimer()
	for b.Loop() {
		cube.RotateX()
	}
}

func BenchmarkConditionMitosis(b *testing.B) {
	manifold := &IcosahedralManifold{}
	for i := 0; i < CubeFaces && i < 10; i++ {
		manifold.Cubes[0][i].Set(0)
		manifold.Cubes[0][i].Set(100)
	}
	b.ResetTimer()
	for b.Loop() {
		_ = manifold.ConditionMitosis()
	}
}
