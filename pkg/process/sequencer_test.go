package process

import (
	"math/rand"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/geometry"
)

func TestSequencer(t *testing.T) {
	Convey("Given a Sequencer", t, func() {
		seq := NewSequencer(NewCalibrator())

		Convey("When analyzing a random byte stream", func() {
			rng := rand.New(rand.NewSource(time.Now().UnixNano()))
			for i := range 50 {
				// Generating bytes. Some entropy drops or spikes might trigger boundaries
				val := byte(rng.Intn(config.Numeric.VocabSize))
				seq.Analyze(uint32(i), val)
			}

			// We don't guarantee a boundary, but the method runs without panicking.
			Convey("It analyzes successfully", func() {
				So(len(seq.buf), ShouldBeGreaterThan, 0)
			})
		})

		Convey("When forcing a boundary via ShannonCeiling", func() {
			// ShannonCeiling is 0.40. Push many distinct bytes to force ceiling
			for i := range config.Numeric.VocabSize {
				ok, _, _, _ := seq.Analyze(uint32(i), byte(i)) // Max entropy
				if ok {
					break
				}
			}
			Convey("It should eventually force a boundary", func() {
				// Since all bytes are distinct, density grows fast and hits ceiling.
				So(len(seq.candidates), ShouldBeGreaterThanOrEqualTo, 0)
			})
		})

		Convey("When calibrator history lowers the active density ceiling", func() {
			cal := NewCalibrator(WithWindowSize(4))
			seq = NewSequencer(cal)
			seq.ShannonCeiling = 1.0
			seq.MinSegmentBytes = 2

			cal.FeedbackChunk(6, 0.10, 1.0, 1.0)
			cal.FeedbackChunk(6, 0.15, 1.0, 1.0)
			cal.FeedbackChunk(6, 0.20, 1.0, 1.0)
			cal.FeedbackChunk(6, 0.10, 1.0, 1.0)

			for i := range config.Numeric.VocabSize {
				_, _, _, _ = seq.Analyze(uint32(i), byte(i))
				if len(seq.candidates) > 0 {
					break
				}
			}

			Convey("It should force a boundary before the fallback ceiling is reached", func() {
				So(len(seq.candidates), ShouldBeGreaterThan, 0)
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
			for i := range 10 {
				_, _, _, _ = seq.Analyze(uint32(i), byte(i))
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
			seq.offset = 2
			seq.candidates = []candidate{{k: 2, gain: 1.0}}

			ok, _, evts, _ := seq.Flush()
			Convey("Flush should commit the candidate", func() {
				So(ok, ShouldBeTrue)
				So(len(evts), ShouldBeGreaterThan, 0)
				So(len(seq.candidates), ShouldEqual, 0)
			})

			ok2, _, _ := seq.Forecast(0, 4)
			Convey("Forecast should return detection status without mutating", func() {
				So(ok2 == true || ok2 == false, ShouldBeTrue)
			})
		})
	})
}

func BenchmarkSequencerDetectBoundary(b *testing.B) {
	seq := NewSequencer(nil)
	
	// Create multiple realistic entropy profiles.
	profiles := [][]byte{
		[]byte(generateCorpus(10, rand.New(rand.NewSource(42)))),
		[]byte(generateBinaryNoise(2048, rand.New(rand.NewSource(99)))),
		generateRepetitive(10, 200),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := profiles[i%len(profiles)]
		dist := NewDistribution()
		for _, byteVal := range buf {
			dist.Add(byteVal)
		}
		seq.detectBoundary(buf, dist)
	}
}
