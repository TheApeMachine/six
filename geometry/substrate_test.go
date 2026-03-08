package geometry

import (
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
)

func chordFromBytes(text string) data.Chord {
	bytes := []byte(text)
	chord := data.Chord{}
	for _, b := range bytes {
		base := data.BaseChord(b)
		for i := range chord {
			chord[i] |= base[i]
		}
	}
	return chord
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
		fp := dial.EncodeFromChords(ChordSeqFromBytes("hello"))
		chord := chordFromBytes("hello")
		hs.Add(chord, fp, []byte("0: hello"))

		fp2 := dial.EncodeFromChords(ChordSeqFromBytes("world"))
		chord2 := chordFromBytes("world")
		hs.Add(chord2, fp2, []byte("1: world"))

		Convey("When retrieving with matching context", func() {
			ctxChord := chordFromBytes("hello")
			ctxFP := dial.EncodeFromChords(ChordSeqFromBytes("hello"))
			got := hs.Retrieve(ctxChord, ctxFP, 2)
			So(got, ShouldNotBeNil)
			So(string(got), ShouldContainSubstring, "hello")
		})

		Convey("When substrate is empty", func() {
			empty := NewHybridSubstrate()
			So(empty.Retrieve(chord, fp, 5), ShouldBeNil)
		})
	})
}

func TestReadoutText(t *testing.T) {
	Convey("Given ReadoutText", t, func() {
		Convey("When readout has idx: payload format", func() {
			So(ReadoutText([]byte("42: the payload")), ShouldEqual, "the payload")
		})
		Convey("When readout has no separator", func() {
			So(ReadoutText([]byte("raw")), ShouldEqual, "raw")
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
			hs.Add(chord, fp, []byte(fmt.Sprintf("%d: item", i)))
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
		hs.Add(data.Chord{}, PhaseDial{}, []byte("0: a"))
		hs.Add(data.Chord{}, PhaseDial{}, []byte("1: b"))
		hs.Add(data.Chord{}, PhaseDial{}, []byte("2: c"))
		ranked := []CandidateScore{{Idx: 0, Score: 0.9}, {Idx: 1, Score: 0.7}, {Idx: 2, Score: 0.5}}

		Convey("When excluding none", func() {
			So(hs.TopExcluding(ranked), ShouldEqual, 0)
		})
		Convey("When excluding top", func() {
			So(hs.TopExcluding(ranked, "a"), ShouldEqual, 1)
		})
	})
}

func BenchmarkHybridSubstrateAdd(b *testing.B) {
	hs := NewHybridSubstrate()
	dial := NewPhaseDial()
	chord := data.BaseChord('x')
	fp := dial.EncodeFromChords([]data.Chord{chord})
	b.ResetTimer()
	for b.Loop() {
		hs.Add(chord, fp, []byte("entry"))
	}
}

func BenchmarkHybridSubstrateBitwiseFilter(b *testing.B) {
	hs := NewHybridSubstrate()
	dial := NewPhaseDial()
	for i := 0; i < 100; i++ {
		chord := data.BaseChord(byte(i % 256))
		fp := dial.EncodeFromChords([]data.Chord{chord})
		hs.Add(chord, fp, []byte(fmt.Sprintf("%d: item", i)))
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
		hs.Add(chord, fp, []byte(fmt.Sprintf("%d: x", i)))
		candidates[i] = i
	}
	ctxFP := dial.EncodeFromChords([]data.Chord{data.BaseChord(10)})
	b.ResetTimer()
	for b.Loop() {
		_ = hs.PhaseDialScoring(candidates, ctxFP)
	}
}
