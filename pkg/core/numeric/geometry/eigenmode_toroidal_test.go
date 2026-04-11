package geometry

import (
	"math"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestNewEigenMode(t *testing.T) {
	Convey("Given NewEigenMode", t, func() {
		ei := NewEigenMode()
		So(ei, ShouldNotBeNil)
		So(ei.Trained, ShouldBeFalse)
	})
}

func TestEigenModePhaseForValue(t *testing.T) {
	Convey("Given EigenMode.PhaseForValue", t, func() {
		ei := NewEigenMode()
		values, err := primitive.NewValue([]byte("phase"))
		So(err, ShouldBeNil)
		So(len(values), ShouldBeGreaterThan, 0)

		theta, phi := ei.PhaseForValue(values[0])
		So(theta, ShouldBeBetweenOrEqual, -math.Pi, math.Pi)
		So(phi, ShouldBeBetweenOrEqual, 0, 2*math.Pi)
	})
}

func TestEigenModeBuildMultiScaleCooccurrence(t *testing.T) {
	Convey("Given BuildMultiScaleCooccurrence", t, func() {
		ei := NewEigenMode()
		err := ei.BuildMultiScaleCooccurrence(nil)
		So(err, ShouldBeNil)
		So(ei.Trained, ShouldBeTrue)
	})
}

func TestEigenModeWeightedCircularMean(t *testing.T) {
	Convey("Given empty slice", t, func() {
		ei := NewEigenMode()
		p, c := ei.WeightedCircularMean(nil)
		So(p, ShouldEqual, 0)
		So(c, ShouldEqual, 0)
	})
}

func BenchmarkNewEigenMode(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = NewEigenMode()
	}
}

func BenchmarkEigenModePhaseForValue(b *testing.B) {
	ei := NewEigenMode()
	values, err := primitive.NewValue([]byte("benchmark phase payload"))
	if err != nil || len(values) < 1 {
		b.Fatal(err)
	}

	value := values[0]

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = ei.PhaseForValue(value)
	}
}

func BenchmarkBuildMultiScaleCooccurrence(b *testing.B) {
	ei := NewEigenMode()
	payload := []primitive.Value{}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = ei.BuildMultiScaleCooccurrence(payload)
	}
}
