package primitive

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/pkg/system/core"
)

/*
TestFlagOperatorFlags verifies packed operator flags are surfaced correctly.
*/
func TestFlagOperatorFlags(t *testing.T) {
	gc.Convey("Given a value with explicit flag bits in the shell word block", t, func() {
		value, err := New()
		gc.So(err, gc.ShouldBeNil)

		value.SetBlock(config.TotalBlocks-1, uint64((ValueFlagTrajectory|ValueFlagGuard)&0x0FFF)<<shellWordShiftFlags)

		gc.So(value.OperatorFlags(), gc.ShouldEqual, ValueFlagTrajectory|ValueFlagGuard)
		gc.So(value.HasOperatorFlag(ValueFlagTrajectory), gc.ShouldBeTrue)
		gc.So(value.HasOperatorFlag(ValueFlagGuard), gc.ShouldBeTrue)
		gc.So(value.HasOperatorFlag(ValueFlagRouteHint), gc.ShouldBeFalse)
	})
}

/*
TestFlagMutationsViaShell verifies shell mutators toggle expected flags.
*/
func TestFlagMutationsViaShell(t *testing.T) {
	gc.Convey("Given trajectory and guard setters", t, func() {
		value, err := New()
		gc.So(err, gc.ShouldBeNil)

		value.SetTrajectory(3, 7)
		value.SetGuardRadius(9)

		gc.So(value.HasOperatorFlag(ValueFlagTrajectory), gc.ShouldBeTrue)
		gc.So(value.HasOperatorFlag(ValueFlagGuard), gc.ShouldBeTrue)
	})
}

/*
BenchmarkFlagHasOperatorFlag measures flag-check throughput.
*/
func BenchmarkFlagHasOperatorFlag(b *testing.B) {
	value, err := New()
	if err != nil {
		b.Fatalf("allocation failed: %v", err)
	}

	value.SetTrajectory(3, 5)
	value.SetGuardRadius(2)

	b.ResetTimer()

	for b.Loop() {
		_ = value.HasOperatorFlag(ValueFlagTrajectory)
		_ = value.HasOperatorFlag(ValueFlagGuard)
		_ = value.OperatorFlags()
	}
}
