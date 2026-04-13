package geometry

import (
	"math/bits"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewField(t *testing.T) {
	t.Parallel()

	Convey("NewField allocates p lanes for GF(p) tier primes", t, func() {
		So(len(NewField(Mod257).Fields), ShouldEqual, int(Mod257))
		So(len(NewField(Mod8191).Fields), ShouldEqual, int(Mod8191))
		So(len(NewField(Mod65537).Fields), ShouldEqual, int(Mod65537))
	})
}

func TestLaneCountForModulus(t *testing.T) {
	t.Parallel()

	Convey("LaneCountForModulus matches the prime order", t, func() {
		So(LaneCountForModulus(Mod257), ShouldEqual, 257)
		So(LaneCountForModulus(0), ShouldEqual, 0)
	})
}

func BenchmarkNewField(b *testing.B) {
	for b.Loop() {
		_ = NewField(Mod257)
	}
}

func TestAffinityMergeAndDistanceHelpers(t *testing.T) {
	t.Parallel()

	Convey("SimulatedMergedAffinity matches MergeAffinity XOR semantics", t, func() {
		base := &Field{}
		incoming := []uint64{0xff, 0x00, 0x01}

		base.MergeAffinity(incoming)
		mergedOnce := append([]uint64(nil), base.Affinity...)

		other := &Field{Affinity: []uint64{0x0f, 0xff, 0x02}}
		sim := SimulatedMergedAffinity(other.Affinity, incoming)

		other2 := &Field{Affinity: append([]uint64(nil), other.Affinity...)}
		other2.MergeAffinity(incoming)

		So(sim, ShouldResemble, other2.Affinity)
		So(mergedOnce, ShouldResemble, incoming)
	})

	Convey("AffinityHammingDistance counts XOR bits across length mismatch", t, func() {
		So(AffinityHammingDistance([]uint64{0xf}, []uint64{0xf}), ShouldEqual, 0)
		So(AffinityHammingDistance([]uint64{0xf}, []uint64{0x0}), ShouldEqual, bits.OnesCount64(0xf))
		So(AffinityHammingDistance([]uint64{1, 2}, []uint64{1}), ShouldEqual, bits.OnesCount64(2))
	})

	Convey("PredictAffinitySaturationAfterMerge reflects hypothetical popcount", t, func() {
		f := &Field{Affinity: []uint64{0x01}}
		in := []uint64{0x02}

		So(f.PredictAffinitySaturationAfterMerge(in), ShouldEqual, AffinitySaturationOfWords(SimulatedMergedAffinity(f.Affinity, in)))
	})
}
