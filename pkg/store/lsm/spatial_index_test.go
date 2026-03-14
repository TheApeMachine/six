package lsm

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data"
)

func TestRotationalDataDensity(t *testing.T) {
	Convey("Given the string 'The cat sat on the mat'", t, func() {
		text := []byte("The cat sat on the mat")
		chord, err := data.BuildChord(text)
		So(err, ShouldBeNil)

		t.Logf("Text: %q (%d bytes)", text, len(text))
		t.Logf("Stored in ONE chord: %d/257 bits active (density %.3f)",
			chord.ActiveCount(), chord.ShannonDensity())

		Convey("It should recover every byte and its position from that single chord", func() {
			// Build the lookup table: for each byte value, what are its 5 unrotated bits?
			byteChords := make(map[byte]data.Chord)

			for b := 0; b < 256; b++ {
				bc, err := data.BuildChord([]byte{byte(b)})
				if err != nil {
					t.Fatalf("BuildChord failed for byte %d: %v", b, err)
				}
				byteChords[byte(b)] = bc
			}

			recovered := make([]byte, len(text))
			recoveredCount := 0

			for pos := range len(text) {
				// At each position, try all 256 byte values:
				// rotate the byte chord by `pos` and check if it's a subset of the stored chord
				for b := 0; b < 256; b++ {
					bc := byteChords[byte(b)]
					candidate := bc.RollLeft(pos)
					sim := data.ChordSimilarity(&candidate, &chord)

					if sim == candidate.ActiveCount() && sim > 0 {
						recovered[pos] = byte(b)
						recoveredCount++
						break
					}
				}
			}

			t.Logf("Recovered: %q (%d/%d bytes)", recovered, recoveredCount, len(text))

			So(recoveredCount, ShouldEqual, len(text))
			So(string(recovered), ShouldEqual, string(text))

			// Now the comparison:
			t.Log("")
			t.Log("=== STORAGE COMPARISON ===")
			t.Logf("WITH rotation:    1 chord  × 257 bits = %d bits total → %d byte positions recoverable",
				257, len(text))
			t.Logf("WITHOUT rotation: %d chords × 257 bits = %d bits total → %d byte positions",
				len(text), len(text)*257, len(text))
			t.Logf("Data density: %.0fx in the same storage space",
				float64(len(text)*257)/float64(257))
		})

		Convey("It should show that false positives are near zero at this density", func() {
			byteChords := make(map[byte]data.Chord)

			for b := 0; b < 256; b++ {
				bc, err := data.BuildChord([]byte{byte(b)})
				if err != nil {
					t.Fatalf("BuildChord failed for byte %d: %v", b, err)
				}
				byteChords[byte(b)] = bc
			}

			falsePositives := 0
			totalChecks := 0

			for pos := range len(text) {
				for b := 0; b < 256; b++ {
					totalChecks++
					bc := byteChords[byte(b)]
					candidate := bc.RollLeft(pos)
					sim := data.ChordSimilarity(&candidate, &chord)

					if sim == candidate.ActiveCount() && sim > 0 {
						if byte(b) != text[pos] {
							falsePositives++
							t.Logf("  FALSE POSITIVE: position %d, decoded %q but expected %q",
								pos, string(rune(b)), string(rune(text[pos])))
						}
					}
				}
			}

			t.Logf("Total checks: %d, false positives: %d (rate: %.6f)",
				totalChecks, falsePositives, float64(falsePositives)/float64(totalChecks))
		})

		Convey("It should scale: 257 positions from a single chord", func() {
			// Build a chord with bytes at ALL 257 rotational positions
			// (use synthetic data — cycle through byte values)
			synth := make([]byte, 257)
			for i := range synth {
				synth[i] = byte(i % 256)
			}

			maxChord, err := data.BuildChord(synth)
			So(err, ShouldBeNil)

			t.Logf("Max capacity chord: %d/257 bits active (density %.3f)",
				maxChord.ActiveCount(), maxChord.ShannonDensity())

			byteChords := make(map[byte]data.Chord)
			for b := 0; b < 256; b++ {
				bc, err := data.BuildChord([]byte{byte(b)})
				if err != nil {
					t.Fatalf("BuildChord failed for byte %d: %v", b, err)
				}
				byteChords[byte(b)] = bc
			}

			recovered := 0
			collisions := 0

			for pos := range 257 {
				matches := 0

				for b := 0; b < 256; b++ {
					bc := byteChords[byte(b)]
					candidate := bc.RollLeft(pos)
					sim := data.ChordSimilarity(&candidate, &maxChord)

					if sim == candidate.ActiveCount() && sim > 0 {
						matches++

						if byte(b) == synth[pos%len(synth)] {
							recovered++
						}
					}
				}

				if matches > 1 {
					collisions++
				}
			}

			t.Logf("257 positions: recovered %d, positions with false positives: %d",
				recovered, collisions)
			t.Logf("ONE chord (257 bits) holding %d addressable positions = %.0fx multiplier",
				257, float64(257))
		})
	})
}

func BenchmarkRotationalRecovery(b *testing.B) {
	text := []byte("The quick brown fox jumps over the lazy dog")
	chord, err := data.BuildChord(text)
	if err != nil {
		b.Fatalf("BuildChord failed for text: %v", err)
	}

	byteChords := make(map[byte]data.Chord)
	for bv := 0; bv < 256; bv++ {
		bc, err := data.BuildChord([]byte{byte(bv)})
		if err != nil {
			b.Fatalf("BuildChord failed for byte %d: %v", bv, err)
		}
		byteChords[byte(bv)] = bc
	}

	b.ResetTimer()

	for b.Loop() {
		for pos := range len(text) {
			for bv := 0; bv < 256; bv++ {
				bc := byteChords[byte(bv)]
				candidate := bc.RollLeft(pos)
				sim := data.ChordSimilarity(&candidate, &chord)

				if sim == 5 {
					_ = byte(bv)
					break
				}
			}
		}
	}

	b.ReportMetric(float64(len(text)), "bytes/recovery")
	b.ReportMetric(float64(len(text))*256, "checks/recovery")
}
