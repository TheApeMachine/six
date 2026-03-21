package automata

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

/*
TestWavefrontActivateTick verifies that activated cells are returned
by Tick and that ActiveCount reflects the current state.
*/
func TestWavefrontActivateTick(t *testing.T) {
	gc.Convey("Given a wavefront with two activated cells", t, func() {
		wavefront := NewWavefront(WavefrontWithDecayRate(5))

		wavefront.Activate([]byte("cell-1"))
		wavefront.Activate([]byte("cell-2"))

		gc.Convey("It should return both cells on the first tick", func() {
			gc.So(wavefront.ActiveCount(), gc.ShouldEqual, 2)

			active := wavefront.Tick()
			gc.So(len(active), gc.ShouldEqual, 2)
		})
	})
}

/*
TestWavefrontEnergyDecay verifies that cells go to sleep after their
energy is exhausted by repeated ticks.
*/
func TestWavefrontEnergyDecay(t *testing.T) {
	gc.Convey("Given an activated cell with decay rate 2", t, func() {
		wavefront := NewWavefront(WavefrontWithDecayRate(2))
		wavefront.Activate([]byte("cell-1"))

		gc.Convey("It should stay active for one tick then sleep", func() {
			active := wavefront.Tick()
			gc.So(len(active), gc.ShouldEqual, 1)

			active = wavefront.Tick()
			gc.So(len(active), gc.ShouldEqual, 0)
			gc.So(wavefront.ActiveCount(), gc.ShouldEqual, 0)
		})
	})
}

/*
TestWavefrontReactivation verifies that sleeping cells can be re-awakened.
*/
func TestWavefrontReactivation(t *testing.T) {
	gc.Convey("Given a cell that has gone to sleep", t, func() {
		wavefront := NewWavefront(WavefrontWithDecayRate(1))
		wavefront.Activate([]byte("cell-1"))
		wavefront.Tick()
		gc.So(wavefront.ActiveCount(), gc.ShouldEqual, 0)

		gc.Convey("It should wake up when re-activated", func() {
			wavefront.Activate([]byte("cell-1"))
			gc.So(wavefront.ActiveCount(), gc.ShouldEqual, 1)
		})
	})
}

/*
TestWavefrontConvergence verifies convergence detection across the
rolling Hamming delta window.
*/
func TestWavefrontConvergence(t *testing.T) {
	gc.Convey("Given a wavefront with epsilon 2 and window size 3", t, func() {
		wavefront := NewWavefront(
			WavefrontWithEpsilon(2),
			WavefrontWithWindowSize(3),
		)

		gc.Convey("It should converge when all window deltas are below epsilon", func() {
			wavefront.RecordDelta(1)
			wavefront.RecordDelta(0)
			wavefront.RecordDelta(1)
			gc.So(wavefront.Converged(), gc.ShouldBeTrue)
		})

		gc.Convey("It should not converge when any delta meets or exceeds epsilon", func() {
			wavefront.RecordDelta(1)
			wavefront.RecordDelta(2)
			wavefront.RecordDelta(0)
			gc.So(wavefront.Converged(), gc.ShouldBeFalse)
		})

		gc.Convey("It should not converge before the window fills", func() {
			wavefront.RecordDelta(0)
			wavefront.RecordDelta(0)
			gc.So(wavefront.Converged(), gc.ShouldBeFalse)
		})
	})
}

/*
TestWavefrontConvergenceRolling verifies that old deltas scroll out of
the rolling window so convergence can occur after a transient spike.
*/
func TestWavefrontConvergenceRolling(t *testing.T) {
	gc.Convey("Given a full window with one stale high delta", t, func() {
		wavefront := NewWavefront(
			WavefrontWithEpsilon(3),
			WavefrontWithWindowSize(3),
		)

		wavefront.RecordDelta(10)
		wavefront.RecordDelta(1)
		wavefront.RecordDelta(0)
		wavefront.RecordDelta(2)

		gc.Convey("It should converge because the old high delta scrolled out", func() {
			gc.So(wavefront.Converged(), gc.ShouldBeTrue)
		})
	})
}

/*
BenchmarkWavefrontTick measures Tick throughput with 100 active cells.
*/
func BenchmarkWavefrontTick(b *testing.B) {
	wavefront := NewWavefront(WavefrontWithDecayRate(1 << 30))

	for idx := range 100 {
		wavefront.Activate([]byte{byte(idx)})
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		wavefront.Tick()
	}
}

/*
BenchmarkWavefrontActivate measures Activate throughput.
*/
func BenchmarkWavefrontActivate(b *testing.B) {
	wavefront := NewWavefront(WavefrontWithDecayRate(100))

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		wavefront.Activate([]byte("cell-bench"))
	}
}

/*
BenchmarkWavefrontConverged measures convergence check throughput.
*/
func BenchmarkWavefrontConverged(b *testing.B) {
	wavefront := NewWavefront(
		WavefrontWithEpsilon(10),
		WavefrontWithWindowSize(5),
	)

	for range 5 {
		wavefront.RecordDelta(3)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		wavefront.Converged()
	}
}
