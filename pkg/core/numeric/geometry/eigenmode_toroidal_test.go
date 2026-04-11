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
		So(ei.Trained, ShouldBeTrue)
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
