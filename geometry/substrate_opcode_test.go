package geometry

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestGFRotationFace256Source(t *testing.T) {
	Convey("Given the FINALDEMO affine states", t, func() {
		Convey("Face256Source should match the delimiter reverse-map", func() {
			So(IdentityRotation().Face256Source(), ShouldEqual, 256)
			So(RotationX.Face256Source(), ShouldEqual, 255)
			So(RotationY.Face256Source(), ShouldEqual, 171)
			So(RotationZ.Face256Source(), ShouldEqual, 85)
		})
	})
}

func TestGFRotationAffineString(t *testing.T) {
	Convey("Given FINALDEMO affine rendering", t, func() {
		Convey("AffineString should use the compact p notation", func() {
			So(IdentityRotation().AffineString(), ShouldEqual, "p")
			So(RotationX.AffineString(), ShouldEqual, "p+1")
			So(RotationY.AffineString(), ShouldEqual, "3p")
			So(RotationZ.AffineString(), ShouldEqual, "3p+1")
		})
	})
}

func TestFinalDemoRotationTable(t *testing.T) {
	Convey("Given the FINALDEMO discrete rotations", t, func() {
		Convey("Each quarter-turn representative should match the PoC constants", func() {
			So(RotationX180, ShouldEqual, GFRotation{A: 1, B: 2})
			So(RotationX270, ShouldEqual, GFRotation{A: 1, B: 256})
			So(RotationY180, ShouldEqual, GFRotation{A: 9, B: 0})
			So(RotationY270, ShouldEqual, GFRotation{A: 86, B: 0})
			So(RotationZ180, ShouldEqual, GFRotation{A: 9, B: 4})
			So(RotationZ270, ShouldEqual, GFRotation{A: 86, B: 171})
		})
	})
}

func TestDeriveSubstrateOpcode(t *testing.T) {
	Convey("Given FINALDEMO opcode thresholds", t, func() {
		rotationForFace256 := func(face int) GFRotation {
			offset := (CubeFaces - 1 - face + CubeFaces) % CubeFaces
			return GFRotation{A: 1, B: uint16(offset)}
		}

		cases := []struct {
			name   string
			source GFRotation
			target GFRotation
			want   SubstrateOpcode
			band   string
		}{
			{name: "rotate x", source: rotationForFace256(10), target: rotationForFace256(20), want: OpcodeRotateX, band: OpcodeBandRotate},
			{name: "rotate y", source: rotationForFace256(31), target: rotationForFace256(1), want: OpcodeRotateY, band: OpcodeBandRotate},
			{name: "rotate z", source: rotationForFace256(60), target: rotationForFace256(20), want: OpcodeRotateZ, band: OpcodeBandRotate},
			{name: "align", source: rotationForFace256(90), target: rotationForFace256(10), want: OpcodeAlign, band: OpcodeBandStable},
			{name: "search", source: rotationForFace256(100), target: rotationForFace256(50), want: OpcodeSearch, band: OpcodeBandStable},
			{name: "sync", source: rotationForFace256(120), target: rotationForFace256(60), want: OpcodeSync, band: OpcodeBandStable},
			{name: "fork", source: rotationForFace256(110), target: rotationForFace256(100), want: OpcodeFork, band: OpcodeBandGrowth},
			{name: "compose", source: rotationForFace256(120), target: rotationForFace256(110), want: OpcodeCompose, band: OpcodeBandGrowth},
		}

		for _, testCase := range cases {
			testCase := testCase
			Convey("When deriving "+testCase.name, func() {
				opcode := DeriveSubstrateOpcode(testCase.source, testCase.target)
				So(opcode, ShouldEqual, testCase.want)
				So(opcode.Band(), ShouldEqual, testCase.band)
			})
		}
	})
}

func BenchmarkDeriveSubstrateOpcode(b *testing.B) {
	source := RotationY270
	target := RotationZ180

	for b.Loop() {
		_ = DeriveSubstrateOpcode(source, target)
	}
}
