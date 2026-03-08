package geometry

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/numeric"
)

func TestManifoldHeader(t *testing.T) {
	Convey("Given a ManifoldHeader", t, func() {
		var header ManifoldHeader

		Convey("When setting State to 1", func() {
			header.SetState(1)
			So(header.State(), ShouldEqual, uint8(1))
		})

		Convey("When setting State to 0", func() {
			header.SetState(0)
			So(header.State(), ShouldEqual, uint8(0))
		})

		Convey("When setting RotState to 42", func() {
			header.SetRotState(42)
			So(header.RotState(), ShouldEqual, uint8(42))
		})

		Convey("When RotState overflows (65 masks to 6 bits)", func() {
			header.SetRotState(65)
			So(header.RotState(), ShouldEqual, uint8(1))
		})

		Convey("When incrementing Winding through 16 steps", func() {
			for i := 0; i < 16; i++ {
				So(header.Winding(), ShouldEqual, uint8(i))
				header.IncrementWinding()
			}
			So(header.Winding(), ShouldEqual, uint8(0))
		})

		Convey("When ResetWinding after increments", func() {
			header.IncrementWinding()
			header.IncrementWinding()
			So(header.Winding(), ShouldEqual, uint8(2))
			header.ResetWinding()
			So(header.Winding(), ShouldEqual, uint8(0))
		})

		Convey("When State, RotState and Winding are set together", func() {
			header.SetState(1)
			header.SetRotState(59)
			header.IncrementWinding()
			header.IncrementWinding()
			header.IncrementWinding()
			So(header.State(), ShouldEqual, uint8(1))
			So(header.RotState(), ShouldEqual, uint8(59))
			So(header.Winding(), ShouldEqual, uint8(3))
		})
	})
}

func TestIcosahedralManifold_LayoutSize(t *testing.T) {
	Convey("Given IcosahedralManifold", t, func() {
		Convey("When checking layout size", func() {
			So(int(unsafe.Sizeof(IcosahedralManifold{})), ShouldEqual, numeric.ManifoldBytes)
		})
	})
}

func BenchmarkManifoldHeaderOps(b *testing.B) {
	var header ManifoldHeader
	n := 0
	for b.Loop() {
		header.SetState(uint8(n & 1))
		header.SetRotState(uint8(n % 60))
		header.IncrementWinding()
		_ = header.State()
		_ = header.RotState()
		_ = header.Winding()
		n++
	}
}
