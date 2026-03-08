package geometry

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestGeodesicLUT_StateCount(t *testing.T) {
	Convey("Given the initialized geodesic tables", t, func() {
		Convey("When checking UnifiedGeodesicMatrix size", func() {
			So(len(UnifiedGeodesicMatrix), ShouldEqual, 3600)
		})

		Convey("When checking StateTransitionMatrix dimensions", func() {
			So(len(StateTransitionMatrix), ShouldEqual, 60)
			for stateIdx := range StateTransitionMatrix {
				So(len(StateTransitionMatrix[stateIdx]), ShouldEqual, 4)
			}
		})
	})
}

func TestGeodesicLUT_DiagonalZero(t *testing.T) {
	Convey("Given UnifiedGeodesicMatrix", t, func() {
		Convey("When inspecting diagonal elements", func() {
			for i := 0; i < 60; i++ {
				So(UnifiedGeodesicMatrix[i*60+i], ShouldEqual, 0)
			}
		})
	})
}

func TestGeodesicLUT_TransitionsValid(t *testing.T) {
	Convey("Given StateTransitionMatrix", t, func() {
		Convey("When every transition is applied", func() {
			for stateIdx := 0; stateIdx < 60; stateIdx++ {
				for eventIdx := 0; eventIdx < 4; eventIdx++ {
					next := StateTransitionMatrix[stateIdx][eventIdx]
					So(next, ShouldBeBetweenOrEqual, 0, 59)
				}
			}
		})
	})
}

func TestGeodesicLUT_Reachable(t *testing.T) {
	Convey("Given UnifiedGeodesicMatrix", t, func() {
		Convey("When checking state 0 can reach all others", func() {
			for j := 0; j < 60; j++ {
				d := UnifiedGeodesicMatrix[j]
				So(d != 255, ShouldBeTrue)
			}
		})
	})
}

func BenchmarkGeodesicLUT_Lookup(b *testing.B) {
	for b.Loop() {
		_ = UnifiedGeodesicMatrix[17*60+42]
	}
}

func BenchmarkGeodesicLUT_StateTransition(b *testing.B) {
	idx := 0
	for b.Loop() {
		_ = StateTransitionMatrix[idx%60][idx%4]
		idx++
	}
}
