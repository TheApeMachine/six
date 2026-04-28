package primitive

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func TestWithLabels(t *testing.T) {
	Convey("Given multiple labels", t, func() {
		value := Emit(WithLabels(0, 42, 99))
		defer value.Close()

		Convey("It should apply each non-zero label to LABELS (last write wins on the single word)", func() {
			label, err := value.Property(LABELS)
			confidence, confidenceErr := value.Property(CONFIDENCE)

			So(err, ShouldBeNil)
			So(confidenceErr, ShouldBeNil)
			So(label, ShouldEqual, 99)
			So(confidence, ShouldEqual, 0)
		})
	})
}

func TestWithFirmware(t *testing.T) {
	Convey("Given known firmware", t, func() {
		value := Emit(WithFirmware(core.QUERY))
		defer value.Close()

		Convey("It should install executable program words", func() {
			So(value.ReadyForALU(), ShouldBeTrue)
			So(value.HasProgram(), ShouldBeTrue)
		})
	})

	Convey("Given unknown firmware", t, func() {
		value := Emit()
		defer value.Close()

		Convey("It should fail loudly instead of leaving a no-op Value", func() {
			So(func() {
				WithFirmware(core.FirmwareType("missing"))(value)
			}, ShouldPanic)
		})
	})
}

func BenchmarkWithLabels(b *testing.B) {
	for b.Loop() {
		value := Emit(WithLabels(1, 2, 3))
		value.Close()
	}
}

func BenchmarkWithFirmware(b *testing.B) {
	for b.Loop() {
		value := Emit(WithFirmware(core.QUERY))
		value.Close()
	}
}
