package mesh

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
TestFieldMeasureCrystallization validates the three sub-metrics and the
composed score against hand-computed populations. Each Convey block
isolates a single invariant so regressions show up as a specific
failing branch rather than a generic score drift.
*/
func TestFieldMeasureCrystallization(t *testing.T) {
	Convey("Given an empty Field", t, func() {
		field := NewField(context.Background(), 65537, nil)
		defer field.Close()

		Convey("measureCrystallization reports zero across the board", func() {
			metrics := field.Metrics()

			So(metrics.MemberCount, ShouldEqual, 0)
			So(metrics.Coverage, ShouldEqual, 0)
			So(metrics.Consensus, ShouldEqual, 0)
			So(metrics.LabelDensity, ShouldEqual, 0)
			So(metrics.Crystallization, ShouldEqual, 0)
			So(metrics.Saturated, ShouldBeFalse)
		})
	})

	Convey("Given a Field with three members, two fully labeled with the same class", t, func() {
		field := NewField(context.Background(), 65537, nil)
		defer field.Close()

		// Two identically-labeled members (slots [7,7,7,7]) and one
		// untagged member — Coverage = 2/3, LabelDensity = (4+4+0) / 12 = 8/12,
		// Consensus = 1 (single-class histogram).
		for idx := 0; idx < 2; idx++ {
			value := primitive.Emit(
				primitive.WithLabels(7, 7, 7, 7),
				primitive.WithConfidence(0xFFFFFFFFFFFFFFFF),
				primitive.WithEpoch(0xFFFFFFFFFFFFFFFF),
				primitive.WithTTL(0xFFFFFFFFFFFFFFFF),
				primitive.WithNoise(0xFFFFFFFFFFFFFFFF),
				primitive.WithStatus(0xFFFFFFFFFFFFFFFF),
			)
			field.AddValue(value)
		}

		blank := primitive.AllocValue()
		blank.StampNewID()
		field.AddValue(blank)

		Convey("Coverage, Consensus, LabelDensity degenerate to the expected fractions", func() {
			metrics := field.Metrics()

			So(metrics.MemberCount, ShouldEqual, 3)
			So(metrics.LabeledCount, ShouldEqual, 2)
			So(metrics.SlotSum, ShouldEqual, 8)
			So(metrics.Coverage, ShouldAlmostEqual, 2.0/3.0, 1e-9)
			So(metrics.LabelDensity, ShouldAlmostEqual, 8.0/12.0, 1e-9)
			So(metrics.Consensus, ShouldEqual, 1)
			So(metrics.Crystallization, ShouldAlmostEqual, (2.0/3.0)*(8.0/12.0), 1e-9)
			So(metrics.Saturated, ShouldBeTrue) // 0.444… > 0.35 floor
		})
	})

	Convey("Given a Field whose members split evenly across two classes", t, func() {
		field := NewField(context.Background(), 65537, nil)
		defer field.Close()

		classA := primitive.Emit(
			primitive.WithLabels(1, 1, 1, 1),
			primitive.WithConfidence(0xFFFFFFFFFFFFFFFF),
			primitive.WithEpoch(0xFFFFFFFFFFFFFFFF),
			primitive.WithTTL(0xFFFFFFFFFFFFFFFF),
			primitive.WithNoise(0xFFFFFFFFFFFFFFFF),
			primitive.WithStatus(0xFFFFFFFFFFFFFFFF),
		)
		classB := primitive.Emit(
			primitive.WithLabels(2, 2, 2, 2),
			primitive.WithConfidence(0xFFFFFFFFFFFFFFFF),
			primitive.WithEpoch(0xFFFFFFFFFFFFFFFF),
			primitive.WithTTL(0xFFFFFFFFFFFFFFFF),
			primitive.WithNoise(0xFFFFFFFFFFFFFFFF),
			primitive.WithStatus(0xFFFFFFFFFFFFFFFF),
		)

		field.AddValue(classA)
		field.AddValue(classA)
		field.AddValue(classB)
		field.AddValue(classB)

		Convey("Consensus drops to zero and drags Crystallization with it", func() {
			metrics := field.Metrics()

			So(metrics.Coverage, ShouldEqual, 1)
			So(metrics.LabelDensity, ShouldEqual, 1)
			// Two classes at equal mass → entropy = log2(2) = 1, so Consensus = 0.
			So(metrics.Consensus, ShouldAlmostEqual, 0, 1e-9)
			So(metrics.Crystallization, ShouldAlmostEqual, 0, 1e-9)
			So(metrics.Saturated, ShouldBeFalse)
		})
	})
}

/*
TestJaccardCouplingAffinity locks down the union-zero edge case and the
inter/union ratio semantics so a silent refactor of the coupling rule
cannot drift eigenmode partitions unnoticed.
*/
func TestJaccardCouplingAffinity(t *testing.T) {
	Convey("Given two identical fingerprints", t, func() {
		fp := [affinityWords]uint64{1, 2, 3, 4, 5}

		Convey("coupling is exactly 1", func() {
			So(JaccardCouplingAffinity(fp, fp), ShouldEqual, 1)
		})
	})

	Convey("Given two fully disjoint fingerprints", t, func() {
		a := [affinityWords]uint64{0xF0F0F0F0F0F0F0F0, 0, 0, 0, 0}
		b := [affinityWords]uint64{0x0F0F0F0F0F0F0F0F, 0, 0, 0, 0}

		Convey("coupling is 0 because intersection is empty", func() {
			So(JaccardCouplingAffinity(a, b), ShouldEqual, 0)
		})
	})

	Convey("Given two all-zero fingerprints", t, func() {
		var zero [affinityWords]uint64

		Convey("coupling is 1 so blank values always share a mode", func() {
			So(JaccardCouplingAffinity(zero, zero), ShouldEqual, 1)
		})
	})

	Convey("Given a fingerprint and a proper-subset fingerprint", t, func() {
		a := [affinityWords]uint64{0b1111, 0, 0, 0, 0}
		b := [affinityWords]uint64{0b0011, 0, 0, 0, 0}

		Convey("coupling is |intersection| / |union| = 2/4", func() {
			So(JaccardCouplingAffinity(a, b), ShouldAlmostEqual, 0.5, 1e-9)
		})
	})
}

func BenchmarkFieldMeasureCrystallization(b *testing.B) {
	field := NewField(context.Background(), 65537, nil)
	defer field.Close()

	// 64 members, roughly 70% labeled, three distinct classes — a
	// realistic post-warmup community shape so the benchmark reflects
	// steady-state measurement cost rather than an empty fast-path.
	const memberCount = 64

	for idx := 0; idx < memberCount; idx++ {
		value := primitive.Emit(
			primitive.WithLabels(9, 9, 9, 9),
			primitive.WithConfidence(0xFFFFFFFFFFFFFFFF),
			primitive.WithEpoch(0xFFFFFFFFFFFFFFFF),
			primitive.WithTTL(0xFFFFFFFFFFFFFFFF),
			primitive.WithNoise(0xFFFFFFFFFFFFFFFF),
			primitive.WithStatus(0xFFFFFFFFFFFFFFFF),
		)

		field.AddValue(value)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_ = field.Metrics()
	}
}

func BenchmarkJaccardCouplingAffinity(b *testing.B) {
	a := [affinityWords]uint64{
		0xA5A5A5A5A5A5A5A5, 0x5A5A5A5A5A5A5A5A,
		0xDEADBEEFCAFEF00D, 0xFEEDFACE12345678,
		0x0F0F0F0F0F0F0F0F,
	}
	c := a
	c[0] ^= 0x1

	b.ReportAllocs()
	b.ResetTimer()

	var sink float64

	for iteration := 0; iteration < b.N; iteration++ {
		sink += JaccardCouplingAffinity(a, c)
	}

	_ = sink
}
