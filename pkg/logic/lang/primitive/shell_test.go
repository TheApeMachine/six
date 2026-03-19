package primitive

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
)

/*
TestShellSetStatePhase verifies state phase writes both core bit and carry.
*/
func TestShellSetStatePhase(t *testing.T) {
	gc.Convey("Given a value state phase assignment", t, func() {
		value, err := New()
		gc.So(err, gc.ShouldBeNil)

		value.SetStatePhase(7)
		gc.So(value.ResidualCarry(), gc.ShouldEqual, uint64(7))
		gc.So(primitiveHasBit(value, 7), gc.ShouldBeTrue)

		value.SetStatePhase(0)
		gc.So(value.ResidualCarry(), gc.ShouldEqual, uint64(0))
	})
}

/*
TestShellAffine verifies affine pack/unpack normalization rules.
*/
func TestShellAffine(t *testing.T) {
	gc.Convey("Given a value with affine operator", t, func() {
		value, err := New()
		gc.So(err, gc.ShouldBeNil)

		value.SetAffine(0, 3)
		scale, translate := value.Affine()
		gc.So(scale, gc.ShouldEqual, 1)
		gc.So(translate, gc.ShouldEqual, 3)
	})
}

/*
TestShellTrajectoryAndGuard verifies trajectory and guard metadata.
*/
func TestShellTrajectoryAndGuard(t *testing.T) {
	gc.Convey("Given a value with trajectory and guard", t, func() {
		value, err := New()
		gc.So(err, gc.ShouldBeNil)

		value.SetTrajectory(5, 9)
		from, to, ok := value.Trajectory()
		gc.So(ok, gc.ShouldBeTrue)
		gc.So(from, gc.ShouldEqual, numeric.Phase(5))
		gc.So(to, gc.ShouldEqual, numeric.Phase(9))
		gc.So(value.HasTrajectory(), gc.ShouldBeTrue)

		value.SetGuardRadius(13)
		gc.So(value.GuardRadius(), gc.ShouldEqual, uint8(13))
		gc.So(value.HasGuard(), gc.ShouldBeTrue)
	})
}

/*
TestShellApplyAffinePhase verifies local affine phase execution.
*/
func TestShellApplyAffinePhase(t *testing.T) {
	gc.Convey("Given scale 3 and translate 5", t, func() {
		value, err := New()
		gc.So(err, gc.ShouldBeNil)
		value.SetAffine(3, 5)

		next := value.ApplyAffinePhase(7)
		gc.So(
			next,
			gc.ShouldEqual,
			numeric.Phase((3*7+5)%numeric.FermatPrime),
		)
	})
}

/*
TestShellApplyAffineValue verifies transformed value receives expected state phase.
*/
func TestShellApplyAffineValue(t *testing.T) {
	gc.Convey("Given a value and external affine operator", t, func() {
		value, err := New()
		gc.So(err, gc.ShouldBeNil)
		value.Set(3)
		value.Set(17)

		scale, translate := value.RotationSeed()
		expected := numeric.Phase((uint32(scale)*2 + uint32(translate)*0) % numeric.FermatPrime)
		if expected == 0 {
			expected = 1
		}

		applied := value.ApplyAffineValue(2, 0)

		gc.So(applied.ResidualCarry(), gc.ShouldEqual, uint64(expected))
		gc.So(primitiveHasBit(applied, int(expected)), gc.ShouldBeTrue)
	})
}

/*
BenchmarkShellApplyAffinePhase measures affine phase step throughput.
*/
func BenchmarkShellApplyAffinePhase(b *testing.B) {
	value, err := New()
	if err != nil {
		b.Fatalf("allocation failed: %v", err)
	}

	value.SetAffine(3, 11)
	phase := numeric.Phase(1)

	b.ResetTimer()

	for b.Loop() {
		phase = value.ApplyAffinePhase(phase)
	}
}

/*
BenchmarkShellApplyAffineValue measures full value affine projection.
*/
func BenchmarkShellApplyAffineValue(b *testing.B) {
	value, err := New()
	if err != nil {
		b.Fatalf("allocation failed: %v", err)
	}

	for index := range 32 {
		value.Set(index)
	}

	b.ResetTimer()

	for b.Loop() {
		_ = value.ApplyAffineValue(5, 0)
	}
}
