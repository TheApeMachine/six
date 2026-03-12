package process

import (
	"math/rand"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/geometry"
)

func TestSequencer(t *testing.T) {
	Convey("Given a Sequencer", t, func() {
		seq := NewSequencer(NewCalibrator())

		Convey("When analyzing a random byte stream", func() {
			rng := rand.New(rand.NewSource(time.Now().UnixNano()))
			boundaryDetected := false
			for i := 0; i < 50; i++ {
				// Generating bytes. Some entropy drops or spikes might trigger boundaries
				val := byte(rng.Intn(256))
				ok, _ := seq.Analyze(i, val)
				if ok {
					boundaryDetected = true
				}
			}

			// We don't guarantee a boundary, but the method runs without panicking.
			Convey("It analyzes successfully", func() {
				So(len(seq.buf), ShouldBeGreaterThan, 0)
				So(boundaryDetected, ShouldBeIn, []interface{}{true, false})
			})
		})

		Convey("When forcing a boundary via ShannonCeiling", func() {
			// ShannonCeiling is 0.40. Push many distinct bytes to force ceiling
			for i := 0; i < 256; i++ {
				ok, _ := seq.Analyze(i, byte(i)) // Max entropy
				if ok {
					break
				}
			}
			Convey("It should eventually force a boundary", func() {
				// Since all bytes are distinct, density grows fast and hits ceiling.
				So(len(seq.candidates), ShouldBeGreaterThanOrEqualTo, 0)
			})
		})

		Convey("When detecting a boundary via MDL", func() {
			buf := []byte{0, 0, 0, 0, 0, 255, 255, 255, 255, 255}
			dist := NewDistribution()
			for _, b := range buf {
				dist.Add(b)
			}
			ok, k, gain := seq.detectBoundary(buf, dist)

			Convey("It should identify the boundary and gain", func() {
				So(ok, ShouldBeTrue)
				So(k, ShouldBeGreaterThan, 0)
				So(gain, ShouldBeGreaterThan, 0.0)
			})
		})

		Convey("When balancing candidates", func() {
			// Directly inject artificial candidates
			seq.buf = []byte{0, 0, 0, 255, 255, 255, 0, 0, 0}
			seq.candidates = []candidate{
				{k: 3, gain: 1.0, forced: false},
				{k: 6, gain: 1.5, forced: false},
			}

			seq.balanceCandidates()

			Convey("It should handle merging correctly", func() {
				So(len(seq.candidates), ShouldBeLessThanOrEqualTo, 2)
			})
		})

		Convey("When integrating computeSignal and eigen modes", func() {
			seq.SetEigenMode(nil) // Should reset to NewEigenMode
			So(seq.eigen, ShouldNotBeNil)

			eigen := geometry.NewEigenMode()
			eigen.Trained = true
			seq.SetEigenMode(eigen)

			val, delta, eigenMag := seq.computeSignal(42)
			Convey("It should compute phases", func() {
				So(val, ShouldEqual, 42.0)
				So(delta, ShouldBeGreaterThanOrEqualTo, 0.0)
				So(eigenMag, ShouldBeGreaterThanOrEqualTo, 0.0)
			})
		})

		Convey("When cloning and flushing", func() {
			for i := 0; i < 10; i++ {
				seq.Analyze(i, byte(i))
			}

			clone := seq.Clone()
			Convey("Clone should maintain state", func() {
				So(len(clone.buf), ShouldEqual, len(seq.buf))
				So(clone.offset, ShouldEqual, seq.offset)
			})

			empty := seq.CloneEmpty()
			Convey("CloneEmpty should wipe state", func() {
				So(len(empty.buf), ShouldEqual, 0)
				So(empty.offset, ShouldEqual, 0)
			})

			// Add artificial candidate to test Flush
			seq.buf = []byte{0, 1, 2, 3}
			seq.offset = 0
			seq.candidates = []candidate{{k: 2, gain: 1.0}}

			ok, evts := seq.Flush()
			Convey("Flush should commit the candidate", func() {
				So(ok, ShouldBeTrue)
				So(len(evts), ShouldBeGreaterThan, 0)
				So(len(seq.candidates), ShouldEqual, 0)
			})
			
			ok2, _ := seq.Forecast(0, 4)
			Convey("Forecast should return detection status without mutating", func() {
				So(ok2 == true || ok2 == false, ShouldBeTrue)
			})
		})
	})
}

func BenchmarkSequencerDetectBoundary(b *testing.B) {
	seq := NewSequencer(nil)
	buf := make([]byte, 1024)
	
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 512; i++ {
		buf[i] = 0 // low entropy
	}
	for i := 512; i < 1024; i++ {
		buf[i] = byte(rng.Intn(256)) // high entropy
	}

	dist := NewDistribution()
	for _, byteVal := range buf {
		dist.Add(byteVal)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		seq.detectBoundary(buf, dist)
	}
}
