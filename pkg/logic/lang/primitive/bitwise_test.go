package primitive

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

/*
TestBitwiseOR verifies OR combines core bits from both values.
*/
func TestBitwiseOR(t *testing.T) {
	gc.Convey("Given two sparse values", t, func() {
		left, err := New()
		gc.So(err, gc.ShouldBeNil)
		left.Set(1)
		left.Set(10)

		right, err := New()
		gc.So(err, gc.ShouldBeNil)
		right.Set(10)
		right.Set(20)

		combined, err := left.OR(right)
		gc.So(err, gc.ShouldBeNil)
		gc.So(primitiveHasBit(combined, 1), gc.ShouldBeTrue)
		gc.So(primitiveHasBit(combined, 10), gc.ShouldBeTrue)
		gc.So(primitiveHasBit(combined, 20), gc.ShouldBeTrue)
		gc.So(combined.CoreActiveCount(), gc.ShouldEqual, 3)
	})
}

/*
TestBitwiseXOR verifies XOR cancels shared bits.
*/
func TestBitwiseXOR(t *testing.T) {
	gc.Convey("Given values with overlap", t, func() {
		left, err := New()
		gc.So(err, gc.ShouldBeNil)
		left.Set(1)
		left.Set(10)

		right, err := New()
		gc.So(err, gc.ShouldBeNil)
		right.Set(10)
		right.Set(20)

		xor, err := left.XOR(right)
		gc.So(err, gc.ShouldBeNil)
		gc.So(primitiveHasBit(xor, 1), gc.ShouldBeTrue)
		gc.So(primitiveHasBit(xor, 10), gc.ShouldBeFalse)
		gc.So(primitiveHasBit(xor, 20), gc.ShouldBeTrue)
		gc.So(xor.CoreActiveCount(), gc.ShouldEqual, 2)
	})
}

/*
TestBitwiseHole verifies Hole keeps bits unique to receiver.
*/
func TestBitwiseHole(t *testing.T) {
	gc.Convey("Given a receiver and a subtractor value", t, func() {
		receiver, err := New()
		gc.So(err, gc.ShouldBeNil)
		receiver.Set(5)
		receiver.Set(8)
		receiver.Set(13)

		other, err := New()
		gc.So(err, gc.ShouldBeNil)
		other.Set(8)

		hole, err := receiver.Hole(other)
		gc.So(err, gc.ShouldBeNil)
		gc.So(primitiveHasBit(hole, 5), gc.ShouldBeTrue)
		gc.So(primitiveHasBit(hole, 8), gc.ShouldBeFalse)
		gc.So(primitiveHasBit(hole, 13), gc.ShouldBeTrue)
	})
}

/*
TestBitwiseCountsAndSimilarity verifies core/shell accounting behavior.
*/
func TestBitwiseCountsAndSimilarity(t *testing.T) {
	gc.Convey("Given values with both core and shell bits", t, func() {
		left, err := New()
		gc.So(err, gc.ShouldBeNil)
		left.Set(1)
		left.Set(2)
		left.SetC6(0b1011)

		right, err := New()
		gc.So(err, gc.ShouldBeNil)
		right.Set(2)
		right.Set(3)
		right.SetC6(0b1111)

		gc.So(left.Similarity(right), gc.ShouldEqual, 1)
		gc.So(left.CoreActiveCount(), gc.ShouldEqual, 2)
		gc.So(left.ShellActiveCount(), gc.ShouldEqual, 3)
		gc.So(left.ActiveCount(), gc.ShouldEqual, 5)
	})
}

/*
BenchmarkBitwiseOR measures OR throughput.
*/
func BenchmarkBitwiseOR(b *testing.B) {
	left, _ := New()
	right, _ := New()

	for index := range 64 {
		left.Set(index)
		right.Set((index + 32) % 257)
	}

	b.ResetTimer()

	for b.Loop() {
		if _, err := left.OR(right); err != nil {
			b.Fatalf("or failed: %v", err)
		}
	}
}

/*
BenchmarkBitwiseXOR measures XOR throughput.
*/
func BenchmarkBitwiseXOR(b *testing.B) {
	left, _ := New()
	right, _ := New()

	for index := range 64 {
		left.Set(index)
		right.Set((index + 16) % 257)
	}

	b.ResetTimer()

	for b.Loop() {
		if _, err := left.XOR(right); err != nil {
			b.Fatalf("xor failed: %v", err)
		}
	}
}
