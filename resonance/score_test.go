package resonance

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
)

func TestOverlapScore(t *testing.T) {
	Convey("Given two partially overlapping chords", t, func() {
		left := data.BaseChord('A')
		rightOnly := data.BaseChord('B')
		right := data.ChordOR(&left, &rightOnly)

		Convey("It should return a bounded symmetric overlap score", func() {
			scoreAB := OverlapScore(&left, &right)
			scoreBA := OverlapScore(&right, &left)

			So(scoreAB, ShouldBeGreaterThan, 0)
			So(scoreAB, ShouldBeLessThan, 1.000001)
			So(scoreAB, ShouldAlmostEqual, scoreBA, 0.000001)
		})
	})
}

func TestAffineScore_BoostsCarrierAlignment(t *testing.T) {
	Convey("Given identical structural chords but different carrier overlap", t, func() {
		query := data.BaseChord('Q')
		candidate := data.BaseChord('Q')
		aligned := data.BaseChord('R')
		misaligned := data.BaseChord('Z')

		Convey("It should score aligned carriers higher than misaligned ones", func() {
			good := AffineScore(&query, &candidate, &aligned, &aligned)
			bad := AffineScore(&query, &candidate, &aligned, &misaligned)

			So(good, ShouldBeGreaterThanOrEqualTo, bad)
		})
	})
}

func BenchmarkOverlapScore(b *testing.B) {
	left := data.BaseChord('A')
	rightOnly := data.BaseChord('B')
	right := data.ChordOR(&left, &rightOnly)

	for i := 0; i < b.N; i++ {
		_ = OverlapScore(&left, &right)
	}
}
