package geometry

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
)

// testChords builds a small chord sequence for testing using BaseChord.
func testChords(text string) []data.Chord {
	bytes := []byte(text)
	chords := make([]data.Chord, len(bytes))
	for i, b := range bytes {
		chords[i] = data.BaseChord(b)
	}
	return chords
}

func TestNewHybridSubstrate(t *testing.T) {
	Convey("Given NewHybridSubstrate", t, func() {
		Convey("When creating a fresh substrate", func() {
			hs := NewHybridSubstrate()
			So(hs, ShouldNotBeNil)
			So(len(hs.Entries), ShouldEqual, 0)
		})
	})
}

func TestHybridSubstrateAddRetrieve(t *testing.T) {
	Convey("Given a HybridSubstrate with entries", t, func() {
		hs := NewHybridSubstrate()
		dial := NewPhaseDial()

		chordsA := testChords("hello")
		fpA := dial.EncodeFromChords(chordsA)
		hs.Add(data.ChordLCM(chordsA), fpA, chordsA)

		chordsB := testChords("world")
		fpB := dial.EncodeFromChords(chordsB)
		hs.Add(data.ChordLCM(chordsB), fpB, chordsB)

		Convey("When retrieving with matching context", func() {
			ctxChord := data.ChordLCM(chordsA)
			ctxFP := dial.EncodeFromChords(chordsA)
			got := hs.Retrieve(ctxChord, ctxFP, 2)
			So(got, ShouldNotBeNil)
			So(len(got), ShouldBeGreaterThan, 0)
		})

		Convey("When substrate is empty", func() {
			empty := NewHybridSubstrate()
			So(empty.Retrieve(data.ChordLCM(chordsA), fpA, 5), ShouldBeNil)
		})
	})
}

func TestHybridSubstrateCandidates(t *testing.T) {
	Convey("Given a substrate with N entries", t, func() {
		hs := NewHybridSubstrate()
		dial := NewPhaseDial()
		for i := 0; i < 5; i++ {
			chord := data.BaseChord(byte('a' + i))
			fp := dial.EncodeFromChords([]data.Chord{chord})
			hs.Add(chord, fp, []data.Chord{chord})
		}
		cand := hs.Candidates()
		So(len(cand), ShouldEqual, 5)
		for i := range cand {
			So(cand[i], ShouldEqual, i)
		}
	})
}

func TestTopExcluding(t *testing.T) {
	Convey("Given ranked CandidateScores", t, func() {
		hs := NewHybridSubstrate()
		chA := testChords("a")
		chB := testChords("b")
		chC := testChords("c")
		hs.Add(data.Chord{}, PhaseDial{}, chA)
		hs.Add(data.Chord{}, PhaseDial{}, chB)
		hs.Add(data.Chord{}, PhaseDial{}, chC)
		ranked := []CandidateScore{{Idx: 0, Score: 0.9}, {Idx: 1, Score: 0.7}, {Idx: 2, Score: 0.5}}

		Convey("When excluding none", func() {
			So(hs.TopExcluding(ranked), ShouldEqual, 0)
		})
		Convey("When excluding top", func() {
			So(hs.TopExcluding(ranked, chA), ShouldEqual, 1)
		})
	})
}

func TestHybridSubstrateBitwiseFilter(t *testing.T) {
	Convey("Given substrate entries with different overlap scores", t, func() {
		hs := NewHybridSubstrate()
		dial := NewPhaseDial()

		readoutA := testChords("alpha")
		readoutB := testChords("beta")
		readoutC := testChords("gamma")

		filterA := data.ChordLCM(readoutA)
		filterB := data.ChordLCM(readoutB)
		filterC := data.ChordLCM(readoutC)

		hs.Add(filterA, dial.EncodeFromChords(readoutA), readoutA)
		hs.Add(filterB, dial.EncodeFromChords(readoutB), readoutB)
		hs.Add(filterC, dial.EncodeFromChords(readoutC), readoutC)

		Convey("BitwiseFilter should keep the top scoring indices", func() {
			ranked := hs.BitwiseFilter(filterA, 2)

			So(ranked, ShouldResemble, []int{0, 2})
		})

		Convey("BitwiseFilter should handle zero topK", func() {
			So(hs.BitwiseFilter(filterA, 0), ShouldResemble, []int{})
		})
	})
}

func TestHybridSubstrateRetrieveRanked(t *testing.T) {
	Convey("Given a substrate with indexed lexical metadata", t, func() {
		substrate := NewHybridSubstrate()
		dial := NewPhaseDial()
		chords := testChords("hello")
		fingerprint := dial.EncodeFromChords(chords)

		substrate.AddIndexed(
			data.ChordLCM(chords),
			fingerprint,
			append([]data.Chord(nil), chords...),
			testChords("HELLO"),
			7,
			3,
			true,
		)

		Convey("RetrieveRanked should expose lexical and directional metadata", func() {
			ranked := substrate.RetrieveRanked(data.ChordLCM(chords), fingerprint, 4)

			So(ranked, ShouldHaveLength, 1)
			So(ranked[0].SampleID, ShouldEqual, 7)
			So(ranked[0].Offset, ShouldEqual, 3)
			So(ranked[0].Reverse, ShouldBeTrue)
			So(ranked[0].Lexical, ShouldResemble, testChords("HELLO"))
		})
	})
}

func BenchmarkHybridSubstrateAdd(b *testing.B) {
	hs := NewHybridSubstrate()
	dial := NewPhaseDial()
	chord := data.BaseChord('x')
	fp := dial.EncodeFromChords([]data.Chord{chord})
	readout := []data.Chord{chord}
	b.ResetTimer()
	for b.Loop() {
		hs.Add(chord, fp, readout)
	}
}

func BenchmarkHybridSubstrateBitwiseFilter(b *testing.B) {
	hs := NewHybridSubstrate()
	dial := NewPhaseDial()
	for i := 0; i < 100; i++ {
		chord := data.BaseChord(byte(i % 256))
		fp := dial.EncodeFromChords([]data.Chord{chord})
		hs.Add(chord, fp, []data.Chord{chord})
	}
	ctxChord := data.BaseChord(42)
	b.ResetTimer()
	for b.Loop() {
		_ = hs.BitwiseFilter(ctxChord, 10)
	}
}

func BenchmarkHybridSubstratePhaseDialScoring(b *testing.B) {
	hs := NewHybridSubstrate()
	dial := NewPhaseDial()
	candidates := make([]int, 20)
	for i := 0; i < 20; i++ {
		chord := data.BaseChord(byte(i))
		fp := dial.EncodeFromChords([]data.Chord{chord})
		hs.Add(chord, fp, []data.Chord{chord})
		candidates[i] = i
	}
	ctxFP := dial.EncodeFromChords([]data.Chord{data.BaseChord(10)})
	b.ResetTimer()
	for b.Loop() {
		_ = hs.PhaseDialScoring(candidates, ctxFP)
	}
}
