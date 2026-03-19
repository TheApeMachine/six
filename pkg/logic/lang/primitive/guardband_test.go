package primitive

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

/*
TestGuardbandOpcode verifies low 8-bit opcode extraction from C5.
*/
func TestGuardbandOpcode(t *testing.T) {
	gc.Convey("Given a value with a packed control word", t, func() {
		value, err := New()
		gc.So(err, gc.ShouldBeNil)
		value.SetC5(0xABCD)

		gc.So(value.Opcode(), gc.ShouldEqual, uint64(0xCD))
	})
}

/*
TestGuardbandResidualCarry verifies residual carry read/write in C6.
*/
func TestGuardbandResidualCarry(t *testing.T) {
	gc.Convey("Given a value storing residual carry", t, func() {
		value, err := New()
		gc.So(err, gc.ShouldBeNil)

		value.SetResidualCarry(12345)
		gc.So(value.ResidualCarry(), gc.ShouldEqual, uint64(12345))
	})
}

/*
BenchmarkGuardbandResidualCarry measures carry read/write throughput.
*/
func BenchmarkGuardbandResidualCarry(b *testing.B) {
	value, err := New()
	if err != nil {
		b.Fatalf("allocation failed: %v", err)
	}

	var carry uint64 = 1

	b.ResetTimer()

	for b.Loop() {
		value.SetResidualCarry(carry)
		_ = value.ResidualCarry()
		carry++
	}
}
