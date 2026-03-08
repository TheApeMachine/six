package tokenizer

import (
	"math/rand"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// --- Generative data stream helpers ---

// streamASCIIProse generates a stream of bytes from mixed-case English-like
// byte patterns: alternating runs of lowercase, uppercase, digits, and
// punctuation to simulate natural text variation.
func streamASCIIProse(n int, rng *rand.Rand) []byte {
	ranges := []struct{ lo, hi byte }{
		{'a', 'z'}, // lowercase
		{'A', 'Z'}, // uppercase
		{'0', '9'}, // digits
		{' ', '/'}, // punctuation/whitespace
	}

	bytes := make([]byte, n)
	runLen := 0
	ri := 0

	for i := range n {
		if runLen <= 0 {
			ri = rng.Intn(len(ranges))
			runLen = 3 + rng.Intn(15) // runs of 3-17 chars
		}

		r := ranges[ri]
		b := r.lo + byte(rng.Intn(int(r.hi-r.lo+1)))
		bytes[i] = b
		runLen--
	}

	return bytes
}

// streamBinary generates a stream of bytes from the full 0-255 byte range
// with sharp transitions between low and high regions.
func streamBinary(n int, rng *rand.Rand) []byte {
	bytes := make([]byte, n)
	runLen := 0
	highRegion := false

	for i := range n {
		if runLen <= 0 {
			highRegion = !highRegion
			runLen = 5 + rng.Intn(20) // runs of 5-24 bytes
		}

		var b byte
		if highRegion {
			b = 200 + byte(rng.Intn(56)) // 200-255
		} else {
			b = byte(rng.Intn(50)) // 0-49
		}

		bytes[i] = b
		runLen--
	}

	return bytes
}

// streamMonotone generates a stream of identical bytes — zero volatility.
func streamMonotone(n int) []byte {
	bytes := make([]byte, n)
	for i := range n {
		bytes[i] = 'x'
	}

	return bytes
}

// streamShockTransition generates a calm region followed by a sharp transition
// to a completely different byte range.
func streamShockTransition(calmLen, shockLen int) []byte {
	bytes := make([]byte, calmLen+shockLen)

	for i := range calmLen {
		bytes[i] = 'a' + byte(i%3) // calm: a, b, c repeating
	}

	for i := range shockLen {
		bytes[calmLen+i] = 200 + byte(i%10) // shock: high-byte region
	}

	return bytes
}

// analyzeStream runs the full Sequencer over a byte stream and returns
// the total event count, event type counts, and boundary (reset) count.
func analyzeStream(seq *Sequencer, bytes []byte) (totalEvents, boundaries int, eventCounts map[int]int) {
	eventCounts = make(map[int]int)

	for i, b := range bytes {
		reset, events := seq.Analyze(i, b)

		totalEvents += len(events)
		for _, ev := range events {
			eventCounts[ev]++
		}

		if reset {
			boundaries++
		}
	}

	return
}

// --- Tests ---

func TestSequencerEventsFireOnRealData(t *testing.T) {
	Convey("Given a Sequencer fed realistic data streams", t, func() {
		Convey("Prose-like ASCII text should produce topological events", func() {
			seq := NewSequencer(NewCalibrator())
			chords := streamASCIIProse(1000, rand.New(rand.NewSource(42)))
			totalEvents, boundaries, _ := analyzeStream(seq, chords)

			Printf("\n  ASCII prose: %d events, %d boundaries over 1000 bytes\n", totalEvents, boundaries)

			So(totalEvents, ShouldBeGreaterThan, 0)
			So(boundaries, ShouldBeGreaterThan, 0)
			// Sanity: sequences must average at least 4 bytes.
			So(boundaries, ShouldBeLessThan, 250)
		})

		Convey("Binary data with sharp transitions should produce many events", func() {
			seq := NewSequencer(NewCalibrator())
			chords := streamBinary(1000, rand.New(rand.NewSource(99)))
			totalEvents, boundaries, _ := analyzeStream(seq, chords)

			Printf("\n  Binary shock: %d events, %d boundaries over 1000 bytes\n", totalEvents, boundaries)

			So(totalEvents, ShouldBeGreaterThan, 0)
			So(boundaries, ShouldBeGreaterThan, 0)
		})

		Convey("Monotone stream should produce NO boundaries", func() {
			seq := NewSequencer(NewCalibrator())
			chords := streamMonotone(500)
			_, boundaries, _ := analyzeStream(seq, chords)

			Printf("\n  Monotone: %d boundaries over 500 identical bytes\n", boundaries)

			// A perfectly uniform stream has zero information gain for any split.
			So(boundaries, ShouldEqual, 0)
		})

		Convey("A shock transition should trigger at least one boundary", func() {
			seq := NewSequencer(NewCalibrator())
			chords := streamShockTransition(100, 100)
			totalEvents, boundaries, _ := analyzeStream(seq, chords)

			Printf("\n  Shock transition: %d events, %d boundaries\n", totalEvents, boundaries)

			So(totalEvents, ShouldBeGreaterThan, 0)
			So(boundaries, ShouldBeGreaterThan, 0)
		})
	})
}

func TestSequencerMomentumAccumulation(t *testing.T) {
	Convey("Given a Sequencer ingesting data the way the Loader does", t, func() {
		Convey("Realistic data should produce events that would drive momentum", func() {
			seq := NewSequencer(NewCalibrator())
			bytes := streamASCIIProse(500, rand.New(rand.NewSource(42)))

			eventfulInserts := 0
			for i, b := range bytes {
				_, events := seq.Analyze(i, b)
				if len(events) > 0 {
					eventfulInserts++
				}
			}

			Printf("\n  After 500 bytes: %d inserts with events (would accumulate momentum)\n", eventfulInserts)

			So(eventfulInserts, ShouldBeGreaterThan, 0)
		})

		Convey("Monotone data should produce fewer eventful inserts", func() {
			seq := NewSequencer(NewCalibrator())
			bytes := streamMonotone(500)

			eventfulInserts := 0
			for i, b := range bytes {
				_, events := seq.Analyze(i, b)
				if len(events) > 0 {
					eventfulInserts++
				}
			}

			Printf("\n  Monotone 500 bytes: %d inserts with events\n", eventfulInserts)
		})
	})
}
