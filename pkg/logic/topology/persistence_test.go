package topology

import (
	"math"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
)

/*
TestAddValue verifies that AddValue creates birth events and
increments the component count.
*/
func TestAddValue(t *testing.T) {
	gc.Convey("Given an empty Barcode", t, func() {
		barcode := NewBarcode()

		gc.Convey("It should create birth events at the current threshold", func() {
			barcode.AdvanceThreshold(0.5)
			idA := barcode.AddValue(0)
			idB := barcode.AddValue(1)

			gc.So(idA, gc.ShouldEqual, 0)
			gc.So(idB, gc.ShouldEqual, 1)
			gc.So(len(barcode.Features()), gc.ShouldEqual, 2)
			gc.So(barcode.Features()[0].Birth, gc.ShouldEqual, 0.5)
			gc.So(barcode.Features()[0].Death, gc.ShouldEqual, -1)
			gc.So(barcode.Features()[0].Dimension, gc.ShouldEqual, 0)
			gc.So(barcode.BettiNumbers()[0], gc.ShouldEqual, 2)
		})
	})
}

/*
TestConnect verifies that connecting distinct components creates death
events and that connecting the same component is a no-op.
*/
func TestConnect(t *testing.T) {
	gc.Convey("Given a Barcode with three components", t, func() {
		barcode := NewBarcode()
		barcode.AdvanceThreshold(0.8)
		idA := barcode.AddValue(0)
		idB := barcode.AddValue(1)
		idC := barcode.AddValue(2)

		gc.Convey("It should record a death when merging distinct components", func() {
			barcode.AdvanceThreshold(0.5)
			merged := barcode.Connect(idA, idB)

			gc.So(merged, gc.ShouldBeTrue)
			gc.So(barcode.BettiNumbers()[0], gc.ShouldEqual, 2)

			deathFound := false

			for _, feature := range barcode.Features() {
				if feature.Dimension == 0 && feature.Death == 0.5 {
					deathFound = true
					gc.So(feature.Birth, gc.ShouldEqual, 0.8)
				}
			}

			gc.So(deathFound, gc.ShouldBeTrue)
		})

		gc.Convey("It should not merge the same component twice", func() {
			barcode.Connect(idA, idB)

			gc.So(barcode.Connect(idA, idB), gc.ShouldBeFalse)
			gc.So(barcode.BettiNumbers()[0], gc.ShouldEqual, 2)
		})

		gc.Convey("It should merge all into one component", func() {
			barcode.AdvanceThreshold(0.3)
			barcode.Connect(idA, idB)
			barcode.Connect(idB, idC)

			gc.So(barcode.BettiNumbers()[0], gc.ShouldEqual, 1)
		})
	})
}

/*
TestSweepPair verifies that SweepPair connects values with high
similarity and skips those below the threshold.
*/
func TestSweepPair(t *testing.T) {
	gc.Convey("Given two Values with known overlap", t, func() {
		valA, err := primitive.New()
		gc.So(err, gc.ShouldBeNil)

		valB, err := primitive.New()
		gc.So(err, gc.ShouldBeNil)

		for bit := 0; bit < 100; bit++ {
			valA.Set(bit)
			valB.Set(bit)
		}

		for bit := 100; bit < 110; bit++ {
			valA.Set(bit)
		}

		for bit := 110; bit < 120; bit++ {
			valB.Set(bit)
		}

		barcode := NewBarcode()
		barcode.AdvanceThreshold(1.0)
		idA := barcode.AddValue(0)
		idB := barcode.AddValue(1)

		gc.Convey("It should skip when threshold is too high", func() {
			barcode.AdvanceThreshold(0.95)

			gc.So(barcode.SweepPair(valA, valB, idA, idB), gc.ShouldBeFalse)
			gc.So(barcode.BettiNumbers()[0], gc.ShouldEqual, 2)
		})

		gc.Convey("It should connect when threshold is low enough", func() {
			barcode.AdvanceThreshold(0.5)

			gc.So(barcode.SweepPair(valA, valB, idA, idB), gc.ShouldBeTrue)
			gc.So(barcode.BettiNumbers()[0], gc.ShouldEqual, 1)
		})
	})
}

/*
TestSweep verifies that a full filtration sweep produces a valid barcode
with the correct number of birth and death events.
*/
func TestSweep(t *testing.T) {
	gc.Convey("Given a small set of Values with varying overlap", t, func() {
		valA := primitive.BaseValue(0x41)
		valB := primitive.BaseValue(0x42)
		valC := primitive.BaseValue(0x43)

		barcode := NewBarcode()
		features := barcode.Sweep([]primitive.Value{valA, valB, valC})

		gc.Convey("It should produce exactly 3 birth events", func() {
			births := 0

			for _, feature := range features {
				if feature.Dimension == 0 {
					births++
				}
			}

			gc.So(births, gc.ShouldEqual, 3)
		})

		gc.Convey("It should end with one connected component", func() {
			gc.So(barcode.BettiNumbers()[0], gc.ShouldEqual, 1)
		})

		gc.Convey("It should have exactly 2 death events (3 births - 1 survivor)", func() {
			deaths := 0

			for _, feature := range features {
				if feature.Dimension == 0 && feature.Death >= 0 {
					deaths++
				}
			}

			gc.So(deaths, gc.ShouldEqual, 2)
		})
	})
}

/*
TestBettiNumbers verifies that H_0 matches the UnionFind component count.
*/
func TestBettiNumbers(t *testing.T) {
	gc.Convey("Given a Barcode with known topology", t, func() {
		barcode := NewBarcode()
		barcode.AddValue(0)
		barcode.AddValue(1)
		barcode.AddValue(2)

		gc.Convey("It should report H_0 = 3 before any connections", func() {
			betti := barcode.BettiNumbers()
			gc.So(betti[0], gc.ShouldEqual, 3)
		})

		gc.Convey("It should report H_0 = 2 after one connection", func() {
			barcode.Connect(0, 1)
			betti := barcode.BettiNumbers()
			gc.So(betti[0], gc.ShouldEqual, 2)
		})
	})
}

/*
TestStableFeatures verifies that noise filtering removes short-lived
features while retaining persistent ones.
*/
func TestStableFeatures(t *testing.T) {
	gc.Convey("Given a Barcode with features of varying persistence", t, func() {
		barcode := NewBarcode()

		barcode.AdvanceThreshold(0.9)
		barcode.AddValue(0)
		barcode.AddValue(1)
		barcode.AddValue(2)

		barcode.AdvanceThreshold(0.85)
		barcode.Connect(0, 1)

		barcode.AdvanceThreshold(0.1)
		barcode.Connect(0, 2)

		gc.Convey("It should return only features above the persistence threshold", func() {
			stable := barcode.StableFeatures(0.5)
			noise := barcode.StableFeatures(0.01)

			gc.So(len(stable), gc.ShouldBeGreaterThan, 0)
			gc.So(len(noise), gc.ShouldBeGreaterThanOrEqualTo, len(stable))

			for _, feature := range stable {
				gc.So(feature.Persistence(), gc.ShouldBeGreaterThanOrEqualTo, 0.5)
			}
		})
	})
}

/*
TestBirthDeathPersistence verifies persistence calculation for normal
and essential features.
*/
func TestBirthDeathPersistence(t *testing.T) {
	gc.Convey("Given birth-death pairs", t, func() {
		gc.Convey("It should compute persistence as Death - Birth", func() {
			bd := BirthDeath{Birth: 0.2, Death: 0.8}
			gc.So(bd.Persistence(), gc.ShouldAlmostEqual, 0.6, 1e-12)
		})

		gc.Convey("It should return +Inf for essential features", func() {
			bd := BirthDeath{Birth: 0.5, Death: -1}
			gc.So(math.IsInf(bd.Persistence(), 1), gc.ShouldBeTrue)
		})
	})
}

/*
TestJaccardSimilarity verifies the Jaccard index computation on
primitive Values with known bit patterns.
*/
func TestJaccardSimilarity(t *testing.T) {
	gc.Convey("Given two Values with controlled overlap", t, func() {
		valA, err := primitive.New()
		gc.So(err, gc.ShouldBeNil)

		valB, err := primitive.New()
		gc.So(err, gc.ShouldBeNil)

		for bit := 0; bit < 50; bit++ {
			valA.Set(bit)
			valB.Set(bit)
		}

		for bit := 50; bit < 100; bit++ {
			valA.Set(bit)
		}

		for bit := 100; bit < 150; bit++ {
			valB.Set(bit)
		}

		gc.Convey("It should compute the correct Jaccard index", func() {
			similarity := jaccardSimilarity(valA, valB)
			gc.So(similarity, gc.ShouldAlmostEqual, 50.0/150.0, 1e-12)
		})
	})

	gc.Convey("Given two empty Values", t, func() {
		valA, err := primitive.New()
		gc.So(err, gc.ShouldBeNil)

		valB, err := primitive.New()
		gc.So(err, gc.ShouldBeNil)

		gc.Convey("It should return 0 to avoid division by zero", func() {
			gc.So(jaccardSimilarity(valA, valB), gc.ShouldEqual, 0)
		})
	})

	gc.Convey("Given two identical Values", t, func() {
		valA, err := primitive.New()
		gc.So(err, gc.ShouldBeNil)

		for bit := 0; bit < 200; bit++ {
			valA.Set(bit)
		}

		gc.Convey("It should return 1.0", func() {
			gc.So(jaccardSimilarity(valA, valA), gc.ShouldAlmostEqual, 1.0, 1e-12)
		})
	})
}

/*
BenchmarkJaccardSimilarity measures Jaccard computation throughput for
two dense Values.
*/
func BenchmarkJaccardSimilarity(b *testing.B) {
	valA, _ := primitive.New()
	valB, _ := primitive.New()

	for bit := 0; bit < 500; bit++ {
		valA.Set(bit)
		valB.Set(bit + 250)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		jaccardSimilarity(valA, valB)
	}
}

/*
BenchmarkSweep measures full filtration sweep throughput on a small
Value set.
*/
func BenchmarkSweep(b *testing.B) {
	values := make([]primitive.Value, 8)

	for idx := range values {
		values[idx] = primitive.BaseValue(byte(idx * 31))
	}

	barcode := NewBarcode()
	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		barcode.Sweep(values)
	}
}
