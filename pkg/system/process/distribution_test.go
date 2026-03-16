package process

import (
	"math"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewDistribution(t *testing.T) {
	Convey("Given NewDistribution", t, func() {
		dist := NewDistribution()
		Convey("It should return zeroed counts and initial fields", func() {
			So(dist.n, ShouldEqual, 0)
			So(dist.numDistinct, ShouldEqual, 0)
			So(dist.sumSLogC, ShouldEqual, 0)
			for idx := range dist.counts {
				So(dist.counts[idx], ShouldEqual, 0)
			}
		})
	})
}

func TestDistributionClone(t *testing.T) {
	Convey("Given a Distribution with data", t, func() {
		orig := NewDistribution()
		orig.Add(0)
		orig.Add(1)
		orig.Add(1)

		Convey("When Clone is called", func() {
			cloned := orig.Clone()
			Convey("It should produce an independent copy", func() {
				So(cloned.n, ShouldEqual, orig.n)
				So(cloned.numDistinct, ShouldEqual, orig.numDistinct)
			})
			Convey("Mutating the clone should not affect the original", func() {
				cloned.Add(2)
				So(cloned.n, ShouldEqual, 4)
				So(orig.n, ShouldEqual, 3)
				So(orig.counts[2], ShouldEqual, 0)
			})
		})
	})
}

func TestDistributionAddRemove(t *testing.T) {
	Convey("Given a Distribution", t, func() {
		dist := NewDistribution()
		Convey("When Add is called for a single byte", func() {
			dist.Add(42)
			Convey("It should update counts, n and numDistinct", func() {
				So(dist.counts[42], ShouldEqual, 1)
				So(dist.n, ShouldEqual, 1)
				So(dist.numDistinct, ShouldEqual, 1)
			})
		})
		Convey("When Add is called repeatedly for the same byte", func() {
			for range 5 {
				dist.Add(100)
			}
			Convey("It should accumulate counts", func() {
				So(dist.counts[100], ShouldEqual, 5)
				So(dist.n, ShouldEqual, 5)
				So(dist.numDistinct, ShouldEqual, 1)
			})
		})
		Convey("When Remove is called after Add", func() {
			dist.Add(7)
			dist.Remove(7)
			Convey("It should decrement counts and n", func() {
				So(dist.counts[7], ShouldEqual, 0)
				So(dist.n, ShouldEqual, 0)
				So(dist.numDistinct, ShouldEqual, 0)
			})
		})
	})
}

func TestDistributionCost(t *testing.T) {
	Convey("Given a Distribution with known contents", t, func() {
		dist := NewDistribution()
		for _, b := range []byte{0, 1, 2, 2, 3, 3, 3} {
			dist.Add(b)
		}
		Convey("Cost should equal n*log(n) - sumSLogC", func() {
			n := float64(dist.n)
			expected := n*math.Log(n) - dist.sumSLogC
			So(dist.Cost(), ShouldAlmostEqual, expected, 1e-10)
		})
		Convey("Empty distribution should have zero cost", func() {
			empty := NewDistribution()
			So(empty.Cost(), ShouldEqual, 0)
		})
	})
}

func TestDistributionEntropy(t *testing.T) {
	Convey("Given a Distribution", t, func() {
		Convey("When n is zero", func() {
			dist := NewDistribution()
			Convey("Entropy should be zero", func() {
				So(dist.Entropy(), ShouldEqual, 0)
			})
		})
		Convey("When distribution is uniform over 4 symbols", func() {
			dist := NewDistribution()
			for range 4 {
				dist.Add(0)
				dist.Add(1)
				dist.Add(2)
				dist.Add(3)
			}
			Convey("Entropy should approximate ln(4)", func() {
				expected := math.Log(4)
				So(dist.Entropy(), ShouldAlmostEqual, expected, 0.01)
			})
		})
	})
}

func TestDistributionAddFrom(t *testing.T) {
	Convey("Given two Distributions", t, func() {
		d1 := NewDistribution()
		d1.Add(0)
		d1.Add(1)
		d2 := NewDistribution()
		d2.Add(1)
		d2.Add(2)

		Convey("When AddFrom merges d2 into d1 clone", func() {
			combined := d1.Clone()
			combined.AddFrom(d2)
			Convey("It should have combined counts", func() {
				So(combined.counts[0], ShouldEqual, 1)
				So(combined.counts[1], ShouldEqual, 2)
				So(combined.counts[2], ShouldEqual, 1)
				So(combined.n, ShouldEqual, 4)
				So(combined.numDistinct, ShouldEqual, 3)
			})
		})
	})
}


