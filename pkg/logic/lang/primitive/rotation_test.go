package primitive

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

/*
primitiveHasBit reports whether a core bit index is set.
*/
func primitiveHasBit(value Value, index int) bool {
	blockIndex := index / 64
	bitIndex := uint(index % 64)
	return value.Block(blockIndex)&(uint64(1)<<bitIndex) != 0
}

/*
TestRotationSeed verifies rotation seeds are deterministic and non-zero scale.
*/
func TestRotationSeed(t *testing.T) {
	gc.Convey("Given structurally identical values", t, func() {
		left, err := New()
		gc.So(err, gc.ShouldBeNil)
		left.Set(1)
		left.Set(7)
		left.Set(31)

		right, err := New()
		gc.So(err, gc.ShouldBeNil)
		right.CopyFrom(left)

		leftScale, leftTranslate := left.RotationSeed()
		rightScale, rightTranslate := right.RotationSeed()

		gc.So(leftScale, gc.ShouldEqual, rightScale)
		gc.So(leftTranslate, gc.ShouldEqual, rightTranslate)
		gc.So(leftScale, gc.ShouldNotEqual, 0)
	})
}

/*
TestRollLeft verifies circular motion over the 257-bit core.
*/
func TestRollLeft(t *testing.T) {
	gc.Convey("Given a single active bit", t, func() {
		value, err := New()
		gc.So(err, gc.ShouldBeNil)
		value.Set(0)

		rolled := value.RollLeft(1)
		gc.So(primitiveHasBit(rolled, 1), gc.ShouldBeTrue)
		gc.So(rolled.CoreActiveCount(), gc.ShouldEqual, 1)

		wrapped := value.RollLeft(257)
		gc.So(primitiveHasBit(wrapped, 0), gc.ShouldBeTrue)
	})
}

/*
TestRotate3D verifies rotate transform preserves sparsity cardinality.
*/
func TestRotate3D(t *testing.T) {
	gc.Convey("Given a sparse multi-bit value", t, func() {
		value, err := New()
		gc.So(err, gc.ShouldBeNil)
		value.Set(2)
		value.Set(9)
		value.Set(256)

		rotated := value.Rotate3D()
		gc.So(rotated.CoreActiveCount(), gc.ShouldEqual, 3)
	})
}

/*
BenchmarkRotationSeed measures seed-derivation throughput.
*/
func BenchmarkRotationSeed(b *testing.B) {
	value, err := New()
	if err != nil {
		b.Fatalf("allocation failed: %v", err)
	}

	for index := range 96 {
		value.Set(index)
	}

	b.ResetTimer()

	for b.Loop() {
		_, _ = value.RotationSeed()
	}
}

/*
BenchmarkRollLeft measures roll-left throughput.
*/
func BenchmarkRollLeft(b *testing.B) {
	value, err := New()
	if err != nil {
		b.Fatalf("allocation failed: %v", err)
	}

	for index := range 64 {
		value.Set(index)
	}

	b.ResetTimer()

	shift := 1
	for b.Loop() {
		_ = value.RollLeft(shift)
		shift = (shift + 1) % 257
	}
}
