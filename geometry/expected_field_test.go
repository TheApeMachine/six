package geometry

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewExpectedField_InitializesUnityPrecision(t *testing.T) {
	Convey("Given NewExpectedField", t, func() {
		field := NewExpectedField()
		Convey("When inspecting Precision across all cubes and blocks", func() {
			for cube := range field.Precision {
				for block := range field.Precision[cube] {
					So(field.Precision[cube][block], ShouldEqual, ExpectedPrecisionUnity)
				}
			}
		})
	})
}

func TestExpectedField_ManifoldRoundTrip(t *testing.T) {
	Convey("Given an IcosahedralManifold with populated cubes", t, func() {
		var manifold IcosahedralManifold
		manifold.Header.SetState(1)
		manifold.Header.SetRotState(17)
		manifold.Header.IncrementWinding()
		manifold.Header.IncrementWinding()
		manifold.Cubes[0][0].Set(3)
		manifold.Cubes[1][5].Set(31)
		manifold.Cubes[2][11].Set(67)
		manifold.Cubes[3][26].Set(127)
		manifold.Cubes[4][8].Set(255)

		Convey("When converting to ExpectedField and back", func() {
			field := ExpectedFieldFromManifold(&manifold)
			restored := field.ToManifold()
			So(restored, ShouldResemble, manifold)
		})
	})
}

func TestExpectedFieldFromManifold_NilInput(t *testing.T) {
	Convey("Given nil manifold", t, func() {
		Convey("When ExpectedFieldFromManifold is called", func() {
			got := ExpectedFieldFromManifold(nil)
			So(got, ShouldResemble, NewExpectedField())
		})
	})
}

func TestExpectedManifoldFromField_NilInput(t *testing.T) {
	Convey("Given nil ExpectedField", t, func() {
		Convey("When ExpectedManifoldFromField is called", func() {
			So(ExpectedManifoldFromField(nil), ShouldBeNil)
		})
	})
}

func TestExpectedManifoldFromField_EmptyField(t *testing.T) {
	Convey("Given an empty ExpectedField", t, func() {
		empty := NewExpectedField()
		Convey("When HasSignal is checked", func() {
			So(empty.HasSignal(), ShouldBeFalse)
		})
		Convey("When ExpectedManifoldFromField is called", func() {
			So(ExpectedManifoldFromField(&empty), ShouldBeNil)
		})
	})
}

func TestExpectedManifoldFromField_NonEmptyField(t *testing.T) {
	Convey("Given an ExpectedField with Support[0][0] set", t, func() {
		field := NewExpectedField()
		field.Support[0][0].Set(11)
		Convey("When HasSignal is checked", func() {
			So(field.HasSignal(), ShouldBeTrue)
		})
		Convey("When ExpectedManifoldFromField is called", func() {
			got := ExpectedManifoldFromField(&field)
			So(got, ShouldNotBeNil)
			So(got.Cubes[0][0].Has(11), ShouldBeTrue)
		})
	})
}

func BenchmarkNewExpectedField(b *testing.B) {
	for b.Loop() {
		_ = NewExpectedField()
	}
}

func BenchmarkExpectedFieldFromManifold(b *testing.B) {
	var m IcosahedralManifold
	m.Header.SetState(1)
	m.Cubes[0][0].Set(42)
	b.ResetTimer()
	for b.Loop() {
		_ = ExpectedFieldFromManifold(&m)
	}
}

func BenchmarkExpectedFieldHasSignal(b *testing.B) {
	field := NewExpectedField()
	field.Support[0][0].Set(1)
	b.ResetTimer()
	for b.Loop() {
		_ = field.HasSignal()
	}
}
