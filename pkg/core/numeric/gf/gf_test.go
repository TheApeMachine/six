package gf

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestReduce257(t *testing.T) {
	t.Parallel()

	Convey("Reduce257 folds values into GF(257)", t, func() {
		So(Reduce257(257), ShouldEqual, 0)
		So(Reduce257(258), ShouldEqual, 1)
		So(Reduce257(65536), ShouldEqual, 1)
	})
}

func TestReduce8191(t *testing.T) {
	t.Parallel()

	Convey("Reduce8191 folds values into GF(8191)", t, func() {
		So(Reduce8191(8191), ShouldEqual, 0)
		So(Reduce8191(8192), ShouldEqual, 1)
		So(Reduce8191(16382), ShouldEqual, 0)
	})
}

func TestReduce65537(t *testing.T) {
	t.Parallel()

	Convey("Reduce65537 folds values into GF(65537)", t, func() {
		So(Reduce65537(65537), ShouldEqual, 0)
		So(Reduce65537(65538), ShouldEqual, 1)
		So(Reduce65537(1<<16), ShouldEqual, 65536)
	})
}

func TestVector257ObserveBytes(t *testing.T) {
	t.Parallel()

	Convey("ObserveBytes leaves a non-zero dominant phase", t, func() {
		vector := NewVector257()

		vector.ObserveBytes([]byte("phase"))

		dominant := vector.Dominant()

		So(dominant.Index, ShouldBeGreaterThanOrEqualTo, 0)
		So(dominant.Amplitude, ShouldBeGreaterThan, 0)
		So(dominant.Concentration, ShouldBeGreaterThan, 0)
	})
}

func TestVector257Dot(t *testing.T) {
	t.Parallel()

	Convey("Dot produces constructive interference for identical vectors", t, func() {
		leftVector := LiftBytes([]byte("alpha alpha"))
		rightVector := LiftBytes([]byte("alpha alpha"))

		So(leftVector.Dot(rightVector), ShouldBeGreaterThan, 0)
	})
}

func TestProjectionHierarchy(t *testing.T) {
	t.Parallel()

	Convey("Trie phase projects upward through node and global fields", t, func() {
		localPhase := LiftBytes([]byte("mesh"))
		nodePhase := NewVector8191()
		globalPhase := NewVector65537()

		nodePhase.AccumulateProjected257(localPhase, 0)
		globalPhase.AccumulateProjected8191(nodePhase, 0)

		So(nodePhase.Dominant().Amplitude, ShouldBeGreaterThan, 0)
		So(globalPhase.Dominant().Amplitude, ShouldBeGreaterThan, 0)
	})
}

func TestAlignmentAndInterferenceMultiplier(t *testing.T) {
	t.Parallel()

	Convey("Aligned phases get a larger constructive multiplier", t, func() {
		aligned := InterferenceMultiplier(1, 1)
		misaligned := InterferenceMultiplier(0, 1)

		So(Alignment(10, 10), ShouldEqual, 1)
		So(Alignment(0, 128), ShouldEqual, 0)
		So(aligned, ShouldBeGreaterThan, misaligned)
	})
}

func BenchmarkVector257ObserveBytes(b *testing.B) {
	payload := []byte("benchmark holographic phase")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		vector := NewVector257()
		vector.ObserveBytes(payload)
	}
}

func BenchmarkProjectionHierarchy(b *testing.B) {
	localPhase := LiftBytes([]byte("projection benchmark"))

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		nodePhase := NewVector8191()
		globalPhase := NewVector65537()

		nodePhase.AccumulateProjected257(localPhase, 0)
		globalPhase.AccumulateProjected8191(nodePhase, 0)
	}
}
