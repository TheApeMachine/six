package primitive

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestWithLabels(t *testing.T) {
	Convey("Given multiple labels", t, func() {
		value := Emit(WithLabels(0, 42, 99))
		defer value.Close()

		Convey("It should stamp only the canonical LABELS property", func() {
			label, err := value.Property(LABELS)
			confidence, confidenceErr := value.Property(CONFIDENCE)

			So(err, ShouldBeNil)
			So(confidenceErr, ShouldBeNil)
			So(label, ShouldEqual, 42)
			So(confidence, ShouldEqual, 0)
		})
	})
}

func BenchmarkWithLabels(b *testing.B) {
	for b.Loop() {
		value := Emit(WithLabels(1, 2, 3))
		value.Close()
	}
}
