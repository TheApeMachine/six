package mesh

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
TestFieldDetectEigenmodes validates the partition geometry without
involving Jaccard thresholds directly — the assertions are about
structure (single dominant mode when every fingerprint aligns, two
modes when fingerprints diverge) rather than energy magnitudes.
*/
func TestFieldDetectEigenmodes(t *testing.T) {
	Convey("Given a Field of three near-identical fingerprints", t, func() {
		field := NewField(context.Background(), 65537, nil)
		defer field.Close()

		affStart, _ := core.Cfg.Value.Region.Affinity.WordExtent()
		propsStart, _ := core.Cfg.Value.Region.Properties.WordExtent()

		for idx := 0; idx < 3; idx++ {
			value := primitive.AllocValue()
			value.StampNewID()

			// All five affinity words identical → Jaccard = 1 > threshold
			// → everyone shares one mode.
			for offset := 0; offset < affinityWords; offset++ {
				value.Set(affStart+offset, 0xFFFFFFFFFFFFFFFF)
			}

			// Non-zero surprisal so each participant has positive energy
			// and DetectModes has a meaningful dominant index.
			value.Set(propsStart+1, 0x00FF00FF00FF00FF)

			field.AddValue(value)
		}

		Convey("detectEigenmodes collapses the population into a single mode", func() {
			snap := field.detectEigenmodes()

			So(len(snap.Modes()), ShouldEqual, 1)
			So(snap.DominantIdx(), ShouldEqual, 0)
			So(len(snap.Modes()[0].Members()), ShouldEqual, 3)
		})
	})

	Convey("Given a Field of two disjoint fingerprint groups", t, func() {
		field := NewField(context.Background(), 65537, nil)
		defer field.Close()

		affStart, _ := core.Cfg.Value.Region.Affinity.WordExtent()
		propsStart, _ := core.Cfg.Value.Region.Properties.WordExtent()

		// Group A: top half bits set.
		for idx := 0; idx < 3; idx++ {
			value := primitive.AllocValue()
			value.StampNewID()
			for offset := 0; offset < affinityWords; offset++ {
				value.Set(affStart+offset, 0xFFFFFFFF00000000)
			}
			value.Set(propsStart+1, 0x0F0F0F0F0F0F0F0F)
			field.AddValue(value)
		}

		// Group B: bottom half bits set. Intersection is empty, union is
		// all 64 bits per word → Jaccard = 0 < threshold → separate mode.
		for idx := 0; idx < 2; idx++ {
			value := primitive.AllocValue()
			value.StampNewID()
			for offset := 0; offset < affinityWords; offset++ {
				value.Set(affStart+offset, 0x00000000FFFFFFFF)
			}
			value.Set(propsStart+1, 0xF0F0F0F0F0F0F0F0)
			field.AddValue(value)
		}

		Convey("detectEigenmodes partitions into two orthogonal modes", func() {
			snap := field.detectEigenmodes()

			So(len(snap.Modes()), ShouldEqual, 2)

			// Group A has three members with eight-bit surprisal popcount
			// per word × 8 words = 64; Group B has only two members with
			// the same per-member energy → Group A is the dominant mode.
			So(snap.Modes()[snap.DominantIdx()].Members(), ShouldHaveLength, 3)
		})
	})
}

/*
TestDominantEnergyRatio locks down the bounded [0,1] return semantics so
a regression in the sum-iteration or zero-total fast path is loud.
*/
func TestDominantEnergyRatio(t *testing.T) {
	Convey("Given a nil snap", t, func() {
		Convey("dominantEnergyRatio returns 0 without panicking", func() {
			So(dominantEnergyRatio(nil), ShouldEqual, 0)
		})
	})

	Convey("Given an empty snap", t, func() {
		empty := geometry.NewEigenSnap([]geometry.Eigenmode{}, -1)

		Convey("ratio is 0 because no mode is dominant", func() {
			So(dominantEnergyRatio(empty), ShouldEqual, 0)
		})
	})
}

/*
TestFieldCycle verifies the full Cycle path: metrics are stored under
the atomic pointer and Metrics()/Snap()/Dial() surface the fresh
snapshot the same tick.
*/
func TestFieldCycle(t *testing.T) {
	Convey("Given a populated leaf Field", t, func() {
		field := NewField(context.Background(), 65537, nil)
		defer field.Close()

		propsStart, _ := core.Cfg.Value.Region.Properties.WordExtent()
		affStart, _ := core.Cfg.Value.Region.Affinity.WordExtent()

		for idx := 0; idx < 4; idx++ {
			value := primitive.AllocValue()
			value.StampNewID()
			value.Set(propsStart, kernel.PackClassificationLabelSlots(9, 9, 9, 9))
			for offset := 0; offset < affinityWords; offset++ {
				value.Set(affStart+offset, 0xAAAAAAAAAAAAAAAA)
			}
			value.Set(propsStart+1, 0xFFFFFFFFFFFFFFFF)
			field.AddValue(value)
		}

		Convey("Cycle populates Metrics, Snap, and Dial atomically", func() {
			_, err := field.Cycle()
			So(err, ShouldBeNil)

			metrics := field.Metrics()
			So(metrics.MemberCount, ShouldBeGreaterThanOrEqualTo, 4)
			So(metrics.Coverage, ShouldEqual, 1)
			So(metrics.Consensus, ShouldEqual, 1)
			So(metrics.LabelDensity, ShouldEqual, 1)
			So(metrics.Crystallization, ShouldEqual, 1)
			So(metrics.Saturated, ShouldBeTrue)

			snap := field.Snap()
			So(snap, ShouldNotBeNil)
			So(len(snap.Modes()), ShouldBeGreaterThanOrEqualTo, 1)

			dial := field.Dial()
			So(len(dial), ShouldEqual, geometry.PhaseDialDimensions)
		})

		Convey("Cycle never mutates field.values regardless of saturation", func() {
			before := len(field.Values())
			_, _ = field.Cycle()

			after := len(field.Values())
			So(after, ShouldEqual, before)
		})
	})

	Convey("Given a populated but unlabeled leaf Field", t, func() {
		field := NewField(context.Background(), 65537, nil)
		defer field.Close()

		affStart, _ := core.Cfg.Value.Region.Affinity.WordExtent()

		for idx := 0; idx < 3; idx++ {
			value := primitive.AllocValue()
			value.StampNewID()
			for offset := 0; offset < affinityWords; offset++ {
				value.Set(affStart+offset, 0xAAAAAAAAAAAAAAAA)
			}
			field.AddValue(value)
		}

		/*
		Cycle is observation-only — it never appends carriers to
		field.values because that would feed back into the next
		measurement (carriers carry no labels, so Coverage and
		LabelDensity collapse on every subsequent tick). Carriers
		are minted on demand by BuildPressureCarrier for callers
		that own a gossip routing path.
		*/
		Convey("Cycle marks the field unsaturated without mutating values", func() {
			before := len(field.Values())
			_, err := field.Cycle()
			So(err, ShouldBeNil)

			after := len(field.Values())
			So(after, ShouldEqual, before)

			metrics := field.Metrics()
			So(metrics.Saturated, ShouldBeFalse)
		})

		Convey("BuildPressureCarrier encodes the current metrics on demand", func() {
			_, err := field.Cycle()
			So(err, ShouldBeNil)

			metrics := field.Metrics()
			carrier := field.BuildPressureCarrier(metrics)
			So(carrier, ShouldNotBeNil)

			// The carrier is returned to the caller — NOT appended
			// to field.values — so a subsequent Cycle still sees
			// only the three seeded members.
			So(len(field.Values()), ShouldEqual, 3)
		})
	})
}

func BenchmarkFieldDetectEigenmodes(b *testing.B) {
	field := NewField(context.Background(), 65537, nil)
	defer field.Close()

	affStart, _ := core.Cfg.Value.Region.Affinity.WordExtent()
	propsStart, _ := core.Cfg.Value.Region.Properties.WordExtent()

	for idx := 0; idx < 64; idx++ {
		value := primitive.AllocValue()
		value.StampNewID()

		pattern := uint64(0xA5A5A5A5A5A5A5A5)

		if idx%2 == 1 {
			pattern = ^pattern
		}

		for offset := 0; offset < affinityWords; offset++ {
			value.Set(affStart+offset, pattern)
		}

		value.Set(propsStart+1, pattern)
		field.AddValue(value)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = field.detectEigenmodes()
	}
}
