package store

import (
	"testing"

	bsi "github.com/RoaringBitmap/roaring/v2/BitSliceIndexing"
	"github.com/RoaringBitmap/roaring/v2/roaring64"
	. "github.com/smartystreets/goconvey/convey"

	"github.com/theapemachine/six/pkg/core"
)

func TestTokenRegionWordCount(t *testing.T) {
	Convey("TokenRegionWordCount matches configured token bits", t, func() {
		bits := core.Cfg.Value.Region.Tokens.Bits
		So(bits, ShouldBeGreaterThan, uint64(0))
		So(TokenRegionWordCount(), ShouldEqual, int((bits+63)/64))
	})
}

func TestMaterializeTokenRegionWords(t *testing.T) {
	Convey("MaterializeTokenRegionWords packs row-major token lanes", t, func() {
		idx := NewSpatialIndex()
		tw := TokenRegionWordCount()
		So(tw, ShouldBeGreaterThan, 0)

		tokStart := core.Cfg.Value.Region.Tokens.Start
		a := makeValueWithIDAndPC(7, 6001, 1)
		a[tokStart] = 0xDEADBEEF
		idx.InsertBatch([]uint64{10}, a)

		b := makeValueWithIDAndPC(8, 6002, 2)
		b[tokStart] = 0xCAFEBABE
		idx.InsertBatch([]uint64{10}, b)

		ids := []uint64{6001, 6002}
		buf := idx.MaterializeTokenRegionWords(ids)
		So(len(buf), ShouldEqual, len(ids)*tw)
		So(buf[0], ShouldEqual, 0xDEADBEEF)
		So(buf[tw], ShouldEqual, 0xCAFEBABE)
	})
}

func TestTokenRegionPositionalPopcount(t *testing.T) {
	Convey("TokenRegionPositionalPopcount aggregates set bits across rows", t, func() {
		idx := NewSpatialIndex()
		wc := TokenRegionWordCount()
		if wc <= 0 {
			t.Skip("token words unset")
		}
		x := makeValueWithIDAndPC(20, 7001, 0)
		y := makeValueWithIDAndPC(21, 7002, 0)
		tokStart := core.Cfg.Value.Region.Tokens.Start
		for wordIndex := 0; wordIndex < wc; wordIndex++ {
			x[tokStart+wordIndex] = 0
			y[tokStart+wordIndex] = 0
		}
		tok := tokStart
		x[tok] = 1 // single bit 0 set in first token word
		y[tok] = 2 // bit 1 set
		idx.InsertBatch([]uint64{3}, x)
		idx.InsertBatch([]uint64{3}, y)

		counts := idx.TokenRegionPositionalPopcount([]uint64{7001, 7002})
		So(counts[0], ShouldEqual, 1)
		So(counts[1], ShouldEqual, 1)
	})
}

func TestAndValueIDs(t *testing.T) {
	Convey("AndValueIDs intersects postings", t, func() {
		a := roaring64.New()
		a.Add(1)
		a.Add(2)
		b := roaring64.New()
		b.Add(2)
		b.Add(3)
		out := AndValueIDs(a, b)
		So(out.GetCardinality(), ShouldEqual, uint64(1))
		So(out.Contains(2), ShouldBeTrue)
	})
}

func TestRemoveValueID(t *testing.T) {
	Convey("RemoveValueID purges frame, BSI, and postings", t, func() {
		idx := NewSpatialIndex()
		v := makeValueWithIDAndPC(30, 8001, 5)
		idx.InsertBatch([]uint64{99}, v)
		_, frameOK := idx.FrameByValueID(8001)
		So(frameOK, ShouldBeTrue)

		before := idx.ComparePC(0, bsi.EQ, 5, 0, nil)
		So(before.Contains(8001), ShouldBeTrue)

		idx.RemoveValueID(8001)
		_, ok := idx.FrameByValueID(8001)
		So(ok, ShouldBeFalse)

		after := idx.ComparePC(0, bsi.EQ, 5, 0, nil)
		So(after.Contains(8001), ShouldBeFalse)

		So(idx.ValueIDsForToken(99).Contains(8001), ShouldBeFalse)
	})
}

func BenchmarkMaterializeTokenRegionWords(b *testing.B) {
	idx := NewSpatialIndex()
	ids := make([]uint64, 512)
	for idxStep := range ids {
		v := makeValueWithIDAndPC(uint64(idxStep), uint64(10000+idxStep), 0)
		idx.InsertBatch([]uint64{1}, v)
		ids[idxStep] = uint64(10000 + idxStep)
	}
	b.ResetTimer()
	for benchIteration := 0; benchIteration < b.N; benchIteration++ {
		_ = idx.MaterializeTokenRegionWords(ids)
	}
}
