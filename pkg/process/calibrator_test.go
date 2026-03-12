package process

import (
	"sync"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestCalibrator(t *testing.T) {
	Convey("Given a new Calibrator", t, func() {
		cal := NewCalibrator()

		Convey("It should initialize with default sensitivities", func() {
			So(cal.SensitivityPop(), ShouldEqual, 1.0)
			So(cal.SensitivityPhase(), ShouldEqual, 1.0)
			So(cal.window, ShouldNotBeNil)
		})

		Convey("When testing concurrent access to getters and setters", func() {
			var wg sync.WaitGroup
			for i := 0; i < 100; i++ {
				wg.Add(2)
				go func(val float64) {
					defer wg.Done()
					cal.SetSensitivityPop(val)
					_ = cal.SensitivityPop()
				}(float64(i))
				go func(val float64) {
					defer wg.Done()
					cal.SetSensitivityPhase(val)
					_ = cal.SensitivityPhase()
				}(float64(i))
			}
			wg.Wait()

			Convey("It should not panic or cause race conditions", func() {
				So(cal.SensitivityPop(), ShouldBeGreaterThanOrEqualTo, 0.0)
				So(cal.SensitivityPhase(), ShouldBeGreaterThanOrEqualTo, 0.0)
			})
		})

		Convey("When recalibrating with events", func() {
			cal = NewCalibrator(WithWindowSize(5)) // small window for testing

			Convey("It should early return if window is not warmed", func() {
				cal.Recalibrate([]int{EventDensitySpike})
				So(cal.SensitivityPop(), ShouldEqual, 1.0)
				So(cal.SensitivityPhase(), ShouldEqual, 1.0)
			})

			Convey("It should decay sensitivity if stats are zero", func() {
				for i := 0; i < 5; i++ {
					cal.Recalibrate([]int{EventLowVarianceFlux}) // pushes 0.0
				}
				// 5th push triggers warmed = true, so mean = 0, stddev = 0
				So(cal.SensitivityPop(), ShouldEqual, 0.9)
				So(cal.SensitivityPhase(), ShouldEqual, 0.9)
			})

			Convey("It should adjust sensitivities correctly with mixed events", func() {
				// Warm it up with a mix of 1.0 and 0.0 so mean != 0 and stddev != 0
				cal.Recalibrate([]int{EventDensitySpike})
				cal.Recalibrate([]int{EventDensityTrough})
				cal.Recalibrate([]int{EventLowVarianceFlux})
				cal.Recalibrate([]int{EventPhaseInversion})
				cal.Recalibrate([]int{EventDensitySpike})
				
				pop := cal.SensitivityPop()
				phase := cal.SensitivityPhase()
				
				So(pop, ShouldNotEqual, 1.0)
				So(pop, ShouldBeGreaterThanOrEqualTo, 0.01)
				So(pop, ShouldBeLessThanOrEqualTo, 100.0)
				
				So(phase, ShouldNotEqual, 1.0)
				So(phase, ShouldBeGreaterThanOrEqualTo, 0.01)
				So(phase, ShouldBeLessThanOrEqualTo, 100.0)
			})
		})
	})
}

func BenchmarkRecalibrate(b *testing.B) {
	cal := NewCalibrator(WithWindowSize(128))
	events := []int{EventDensitySpike, EventLowVarianceFlux}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cal.Recalibrate(events)
	}
}
