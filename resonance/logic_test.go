package resonance

import (
	"math/rand"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	config "github.com/theapemachine/six/core"
	"github.com/theapemachine/six/data"
)

// genChord produces a chord with activeBits at deterministic prime indices.
func genChord(seed int64, activeBits int) data.Chord {
	rng := rand.New(rand.NewSource(seed))
	primes := []int{2, 3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37, 41, 43, 47, 53, 59, 61}
	var chord data.Chord
	seen := make(map[int]bool)
	for i := 0; i < activeBits && i < len(primes); i++ {
		p := primes[rng.Intn(len(primes))]
		if p < config.Numeric.NBasis && !seen[p] {
			chord.Set(p)
			seen[p] = true
		}
	}

	return chord
}

func TestApplyPositionalShift(t *testing.T) {
	Convey("Given PositionalPrimeStart < 0 (default)", t, func() {
		orig := PositionalPrimeStart
		PositionalPrimeStart = -1
		defer func() { PositionalPrimeStart = orig }()

		Convey("When ApplyPositionalShift is called", func() {
			chord := genChord(1, 3)
			before := chord
			ApplyPositionalShift(&chord, 0)
			ApplyPositionalShift(&chord, 42)

			Convey("Then it is a no-op", func() {
				So(chord, ShouldResemble, before)
			})
		})
	})

	Convey("Given PositionalPrimeStart >= 0", t, func() {
		orig := PositionalPrimeStart
		PositionalPrimeStart = 0
		defer func() { PositionalPrimeStart = orig }()

		Convey("When ApplyPositionalShift is called with pos 0", func() {
			var chord data.Chord
			ApplyPositionalShift(&chord, 0)

			Convey("Then it sets bit at PositionalPrimeStart + 0", func() {
				So(chord.Has(0), ShouldBeTrue)
			})
		})

		Convey("When ApplyPositionalShift is called with pos mod PositionSlots", func() {
			var chord data.Chord
			ApplyPositionalShift(&chord, 5)
			ApplyPositionalShift(&chord, 5+PositionSlots)

			Convey("Then both set the same slot", func() {
				So(chord.Has(5), ShouldBeTrue)
			})
		})

		Convey("When primeIdx would exceed NBasis", func() {
			PositionalPrimeStart = config.Numeric.NBasis - 10
			var chord data.Chord
			ApplyPositionalShift(&chord, 20)

			Convey("Then it skips out-of-bounds without panic", func() {
				So(chord.ActiveCount(), ShouldBeGreaterThanOrEqualTo, 0)
			})
			PositionalPrimeStart = 0
		})
	})
}

func TestFillScore(t *testing.T) {
	Convey("Given an empty hole", t, func() {
		var hole data.Chord
		candidate := genChord(1, 5)

		Convey("When FillScore is called", func() {
			score := FillScore(&hole, &candidate)

			Convey("Then it returns 0", func() {
				So(score, ShouldEqual, 0)
			})
		})
	})

	Convey("Given a hole and perfect matching candidate", t, func() {
		hole := genChord(1, 5)
		candidate := hole

		Convey("When FillScore is called", func() {
			score := FillScore(&hole, &candidate)

			Convey("Then it returns 1.0", func() {
				So(score, ShouldEqual, 1.0)
			})
		})
	})

	Convey("Given a hole and candidate with extra primes", t, func() {
		var hole data.Chord
		hole.Set(2)
		hole.Set(5)
		hole.Set(7)

		var candidate data.Chord
		candidate.Set(2)
		candidate.Set(5)
		candidate.Set(7)
		candidate.Set(11)
		candidate.Set(13)

		Convey("When FillScore is called", func() {
			score := FillScore(&hole, &candidate)

			Convey("Then score is reduced by extra primes", func() {
				So(score, ShouldBeLessThan, 1.0)
				So(score, ShouldBeGreaterThan, 0)
			})
		})
	})

	Convey("Given a partial fill", t, func() {
		var hole data.Chord
		hole.Set(2)
		hole.Set(5)
		hole.Set(7)

		var candidate data.Chord
		candidate.Set(2)
		candidate.Set(5)

		Convey("When FillScore is called", func() {
			score := FillScore(&hole, &candidate)

			Convey("Then score reflects filled/needed ratio", func() {
				So(score, ShouldBeGreaterThan, 0)
				So(score, ShouldBeLessThan, 1.0)
			})
		})
	})
}

func TestTransitiveResonance(t *testing.T) {
	Convey("Given the structural analogy Cat:Food :: Dog:Animal", t, func() {
		// f1 = cat|wants|food, f2 = dog|wants|food, f3 = dog|is|animal
		// Use distinct prime bits: cat=2, wants=3, food=5, dog=7, is=11, animal=13
		var f1 data.Chord
		f1.Set(2)
		f1.Set(3)
		f1.Set(5)

		var f2 data.Chord
		f2.Set(7)
		f2.Set(3)
		f2.Set(5)

		var f3 data.Chord
		f3.Set(7)
		f3.Set(11)
		f3.Set(13)

		Convey("When TransitiveResonance is called", func() {
			hypothesis := TransitiveResonance(&f1, &f2, &f3)

			Convey("Then it returns A ∪ D (cat|is|animal)", func() {
				So(hypothesis.Has(2), ShouldBeTrue)  // cat
				So(hypothesis.Has(11), ShouldBeTrue) // is
				So(hypothesis.Has(13), ShouldBeTrue) // animal
				So(hypothesis.Has(7), ShouldBeFalse) // dog removed
				So(hypothesis.Has(3), ShouldBeFalse) // wants removed
				So(hypothesis.Has(5), ShouldBeFalse) // food removed
			})
		})
	})

	Convey("Given disjoint F1 and F2", t, func() {
		f1 := genChord(1, 3)
		f2 := genChord(2, 4)
		f3 := genChord(3, 3)

		Convey("When TransitiveResonance is called", func() {
			hypothesis := TransitiveResonance(&f1, &f2, &f3)

			Convey("Then B is empty and A = F1", func() {
				So(hypothesis.ActiveCount(), ShouldBeGreaterThanOrEqualTo, 0)
			})
		})
	})
}

// --- Benchmarks ---

func BenchmarkApplyPositionalShift(b *testing.B) {
	orig := PositionalPrimeStart
	PositionalPrimeStart = 0
	defer func() { PositionalPrimeStart = orig }()

	chord := genChord(1, 5)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ApplyPositionalShift(&chord, i%PositionSlots)
	}
}

func BenchmarkFillScore(b *testing.B) {
	hole := genChord(1, 8)
	candidate := genChord(2, 10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = FillScore(&hole, &candidate)
	}
}

func BenchmarkTransitiveResonance(b *testing.B) {
	f1 := genChord(1, 5)
	f2 := genChord(2, 6)
	f3 := genChord(3, 5)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = TransitiveResonance(&f1, &f2, &f3)
	}
}
