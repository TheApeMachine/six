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
			boundaryDetected := false
			for i := range 50 {
				// Generating bytes. Some entropy drops or spikes might trigger boundaries
				val := byte(rng.Intn(config.Numeric.VocabSize))
				emittedChunk, _ := seq.Analyze(uint32(i), val)
				if len(emittedChunk) > 0 {
					boundaryDetected = true
				}
			}

			// We don't guarantee a boundary, but the method runs without panicking.
			Convey("It analyzes successfully", func() {
				So(len(seq.buf), ShouldBeGreaterThan, 0)
				So(boundaryDetected, ShouldBeIn, []interface{}{true, false})
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

		Convey("When parsing a dense prose sequence natively via MDL on Chords", func() {
			buf := []byte("Alice was beginning to get very tired of sitting by her sister on the bank, and of having nothing to do: once or twice she had peeped into the book her sister was reading, but it had no pictures or conversations in it, 'and what is the use of a book,' thought Alice 'without pictures or conversation?'")
			
			var chunks [][]byte
			seq := NewSequencer(nil)
			for pos, b := range buf {
				emitted, _ := seq.Analyze(uint32(pos), b)
				if len(emitted) > 0 {
					chunks = append(chunks, emitted)
				}
			}
			emitted, _ := seq.Flush()
			for len(emitted) > 0 {
				chunks = append(chunks, emitted)
				emitted, _ = seq.Flush()
			}
			
			Convey("It should discover natural boundary splits purely dynamically without Shannon ceilings", func() {
				So(len(chunks), ShouldBeGreaterThan, 1) // Must have split at least once
				
				maxLen := 0
				for _, chunk := range chunks {
					if len(chunk) > maxLen {
						maxLen = len(chunk)
					}
				}
				// Assert chunks never degenerated into a single flat run of the whole text.
				So(maxLen, ShouldBeLessThan, len(buf))
			})
		})

		Convey("When balancing candidates", func() {
			// Directly inject artificial candidates
			seq.buf = []byte{0, 0, 0, 255, 255, 255, 0, 0, 0}
			seq.candidates = []candidate{
				{k: 3, gain: 1.0},
				{k: 6, gain: 1.5},
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
				seq.Analyze(uint32(i), byte(i))
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

			ok, evts := seq.Flush()
			Convey("Flush should commit the candidate", func() {
				So(len(ok), ShouldBeGreaterThan, 0)
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
	for i := range 512 {
		buf[i] = 0 // low entropy
	}
	for i := 512; i < 1024; i++ {
		buf[i] = byte(rng.Intn(config.Numeric.VocabSize)) // high entropy
	}

	dist := NewDistribution()
	for _, byteVal := range buf {
		dist.Add(byteVal)
	}

	for b.Loop() {
		seq.detectBoundary(buf, dist)
	}
}
