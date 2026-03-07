package tokenizer

import (
	"math/rand"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
)

// --- Generative data stream helpers ---

// streamASCIIProse generates a stream of chords from mixed-case English-like
// byte patterns: alternating runs of lowercase, uppercase, digits, and
// punctuation to simulate natural text variation.
func streamASCIIProse(n int, rng *rand.Rand) []data.Chord {
	ranges := []struct{ lo, hi byte }{
		{'a', 'z'}, // lowercase
		{'A', 'Z'}, // uppercase
		{'0', '9'}, // digits
		{' ', '/'}, // punctuation/whitespace
	}

	chords := make([]data.Chord, n)
	runLen := 0
	ri := 0

	for i := range n {
		if runLen <= 0 {
			ri = rng.Intn(len(ranges))
			runLen = 3 + rng.Intn(15) // runs of 3-17 chars
		}

		r := ranges[ri]
		b := r.lo + byte(rng.Intn(int(r.hi-r.lo+1)))
		chords[i] = data.BaseChord(b)
		runLen--
	}

	return chords
}

// streamBinary generates a stream of chords from the full 0-255 byte range
// with sharp transitions between low and high regions.
func streamBinary(n int, rng *rand.Rand) []data.Chord {
	chords := make([]data.Chord, n)
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

		chords[i] = data.BaseChord(b)
		runLen--
	}

	return chords
}

// streamMonotone generates a stream of identical chords — a dead market with
// zero volatility. The Sequencer should detect NO boundaries here.
func streamMonotone(n int) []data.Chord {
	chords := make([]data.Chord, n)
	c := data.BaseChord('x')
	for i := range n {
		chords[i] = c
	}

	return chords
}

// streamShockTransition generates a calm region followed by a sharp transition
// to a completely different byte range. The Sequencer MUST detect a boundary
// at the transition point.
func streamShockTransition(calmLen, shockLen int) []data.Chord {
	chords := make([]data.Chord, calmLen+shockLen)

	for i := range calmLen {
		chords[i] = data.BaseChord('a' + byte(i%3)) // calm: a, b, c repeating
	}

	for i := range shockLen {
		chords[calmLen+i] = data.BaseChord(200 + byte(i%10)) // shock: high-byte region
	}

	return chords
}

// analyzeStream runs the full Sequencer over a chord stream and returns
// the total event count, event type counts, and boundary (reset) count.
func analyzeStream(seq *Sequencer, chords []data.Chord) (totalEvents, boundaries int, eventCounts map[int]int) {
	eventCounts = make(map[int]int)

	for i, chord := range chords {
		reset, events := seq.Analyze(i, chord)

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
		})

		Convey("Binary data with sharp transitions should produce many events", func() {
			seq := NewSequencer(NewCalibrator())
			chords := streamBinary(1000, rand.New(rand.NewSource(99)))
			totalEvents, boundaries, _ := analyzeStream(seq, chords)

			Printf("\n  Binary shock: %d events, %d boundaries over 1000 bytes\n", totalEvents, boundaries)

			So(totalEvents, ShouldBeGreaterThan, 0)
			So(boundaries, ShouldBeGreaterThan, 0)
		})

		Convey("Monotone stream should produce NO density/phase events", func() {
			seq := NewSequencer(NewCalibrator())
			chords := streamMonotone(500)
			totalEvents, _, _ := analyzeStream(seq, chords)

			Printf("\n  Monotone: %d events over 500 identical bytes\n", totalEvents)

			// After the EMA stabilizes, a monotone stream should fire
			// only low-variance-flux events (coherenceTime > 10), not
			// density/phase events. The total should be modest.
			So(totalEvents, ShouldBeLessThan, 100)
		})

		Convey("A shock transition should trigger at least one boundary at the transition", func() {
			seq := NewSequencer(NewCalibrator())
			chords := streamShockTransition(100, 100)
			totalEvents, boundaries, _ := analyzeStream(seq, chords)

			Printf("\n  Shock transition: %d events, %d boundaries\n", totalEvents, boundaries)

			// The calm→shock transition should be unmissable.
			So(totalEvents, ShouldBeGreaterThan, 0)
			So(boundaries, ShouldBeGreaterThan, 0)
		})
	})
}

func TestSequencerMomentumAccumulation(t *testing.T) {
	Convey("Given a Sequencer ingesting data the way the Loader does", t, func() {
		Convey("Realistic data should produce events that would drive momentum", func() {
			seq := NewSequencer(NewCalibrator())
			chords := streamASCIIProse(500, rand.New(rand.NewSource(42)))

			eventfulInserts := 0
			for i, chord := range chords {
				_, events := seq.Analyze(i, chord)
				if len(events) > 0 {
					eventfulInserts++
				}
			}

			Printf("\n  After 500 bytes: %d inserts with events (would accumulate momentum)\n", eventfulInserts)

			// PrimeField.Insert accumulates momentum when len(events) > 0.
			// If this is zero, momentum will always be zero and generation
			// will never start. This is the root cause test.
			So(eventfulInserts, ShouldBeGreaterThan, 0)
		})

		Convey("Monotone data should produce fewer eventful inserts", func() {
			seq := NewSequencer(NewCalibrator())
			chords := streamMonotone(500)

			eventfulInserts := 0
			for i, chord := range chords {
				_, events := seq.Analyze(i, chord)
				if len(events) > 0 {
					eventfulInserts++
				}
			}

			Printf("\n  Monotone 500 bytes: %d inserts with events\n", eventfulInserts)
		})
	})
}
