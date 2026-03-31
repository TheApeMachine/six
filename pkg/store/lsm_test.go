package store

import (
	"math/rand"
	"testing"

	"github.com/RoaringBitmap/roaring/v2/roaring64"
	. "github.com/smartystreets/goconvey/convey"
)

func TestMergeLevels(t *testing.T) {
	Convey("mergeLevels", t, func() {
		Convey("colliding TokenIDs Or their affinity bitmaps", func() {
			a := &lsmLevel{
				keys:    []uint64{0b0001},
				bitmaps: []*roaring64.Bitmap{roaring64.NewBitmap()},
			}
			a.bitmaps[0].Add(0xAA)

			b := &lsmLevel{
				keys:    []uint64{0b0001},
				bitmaps: []*roaring64.Bitmap{roaring64.NewBitmap()},
			}
			b.bitmaps[0].Add(0xBB)

			merged := mergeLevels(a, b)

			So(len(merged.keys), ShouldEqual, 1)
			So(merged.keys[0], ShouldEqual, 0b0001)
			So(merged.bitmaps[0].Contains(0xAA), ShouldBeTrue)
			So(merged.bitmaps[0].Contains(0xBB), ShouldBeTrue)
			So(merged.bitmaps[0].GetCardinality(), ShouldEqual, 2)
		})

		Convey("disjoint TokenIDs stay separate and sorted", func() {
			a := &lsmLevel{
				keys:    []uint64{0b0010},
				bitmaps: []*roaring64.Bitmap{roaring64.NewBitmap()},
			}
			a.bitmaps[0].Add(0x10)

			b := &lsmLevel{
				keys:    []uint64{0b0001},
				bitmaps: []*roaring64.Bitmap{roaring64.NewBitmap()},
			}
			b.bitmaps[0].Add(0x20)

			merged := mergeLevels(a, b)

			So(len(merged.keys), ShouldEqual, 2)
			So(merged.keys[0], ShouldEqual, 0b0001)
			So(merged.keys[1], ShouldEqual, 0b0010)
			So(merged.bitmaps[0].Contains(0x20), ShouldBeTrue)
			So(merged.bitmaps[1].Contains(0x10), ShouldBeTrue)
		})
	})
}

func TestInsertBatch(t *testing.T) {
	Convey("InsertBatch", t, func() {
		Convey("empty batch is a no-op", func() {
			idx := NewSpatialIndex()
			idx.InsertBatch(nil, nil)
			So(len(idx.levels), ShouldEqual, 0)
		})

		Convey("single batch lands in level 0", func() {
			idx := NewSpatialIndex()
			idx.InsertBatch(
				[]uint64{1, 2},
				[]uint64{0xAA, 0xBB},
			)
			So(len(idx.levels), ShouldEqual, 1)
			So(idx.levels[0], ShouldNotBeNil)
			So(len(idx.levels[0].keys), ShouldEqual, 2)
		})

		Convey("two batches of equal size cascade into level 1", func() {
			idx := NewSpatialIndex()
			idx.InsertBatch([]uint64{1}, []uint64{0xAA})
			idx.InsertBatch([]uint64{2}, []uint64{0xBB})

			// Level 0 should be nil (merged up), level 1 populated.
			So(idx.levels[0], ShouldBeNil)
			So(idx.levels[1], ShouldNotBeNil)
		})

		Convey("duplicate TokenIDs within a batch compress into one affinity bitmap", func() {
			idx := NewSpatialIndex()
			idx.InsertBatch(
				[]uint64{7, 7, 7},
				[]uint64{0xA1, 0xA2, 0xA3},
			)

			bm := idx.ExactLookup(7)
			So(bm.GetCardinality(), ShouldEqual, 3)
			So(bm.Contains(0xA1), ShouldBeTrue)
			So(bm.Contains(0xA2), ShouldBeTrue)
			So(bm.Contains(0xA3), ShouldBeTrue)
		})

		Convey("colliding TokenIDs across batches merge affinity bitmaps", func() {
			idx := NewSpatialIndex()
			idx.InsertBatch([]uint64{9}, []uint64{0xAA})
			idx.InsertBatch([]uint64{9}, []uint64{0xBB})

			bm := idx.ExactLookup(9)
			So(bm.GetCardinality(), ShouldEqual, 2)
			So(bm.Contains(0xAA), ShouldBeTrue)
			So(bm.Contains(0xBB), ShouldBeTrue)
		})
	})
}

func TestQueryHamming(t *testing.T) {
	Convey("QueryHamming", t, func() {
		idx := NewSpatialIndex()

		idx.levels = []*lsmLevel{
			{
				keys:    []uint64{1, 2},
				bitmaps: []*roaring64.Bitmap{bitmapOf(0b0000), bitmapOf(0b0001)},
			},
			{
				keys:    []uint64{3, 4},
				bitmaps: []*roaring64.Bitmap{bitmapOf(0b0010), bitmapOf(0b1111)},
			},
		}

		Convey("distance 0 returns only TokenIDs with an exact affinity match", func() {
			ids := idx.QueryHamming(0b0000, 0)
			So(ids, ShouldResemble, []uint64{1})
		})

		Convey("distance 1 includes TokenIDs whose stored affinities are single-bit neighbors", func() {
			ids := idx.QueryHamming(0b0000, 1)
			So(ids, ShouldResemble, []uint64{1, 2, 3})
		})

		Convey("distance 1 spans across levels", func() {
			ids := idx.QueryHamming(0b0011, 1)
			So(ids, ShouldResemble, []uint64{2, 3})
		})

		Convey("distance 4 catches everything", func() {
			ids := idx.QueryHamming(0b0000, 4)
			So(ids, ShouldResemble, []uint64{1, 2, 3, 4})
		})

		Convey("empty index returns empty bitmap", func() {
			empty := NewSpatialIndex()
			ids := empty.QueryHamming(0xDEAD, 64)
			So(ids, ShouldBeNil)
		})
	})
}

func TestReverseLookup(t *testing.T) {
	Convey("ReverseLookup", t, func() {
		idx := NewSpatialIndex()

		idx.levels = []*lsmLevel{
			{
				keys:    []uint64{10, 20},
				bitmaps: []*roaring64.Bitmap{bitmapOf(0xAA), bitmapOf(0xAA, 0xBB)},
			},
			{
				keys:    []uint64{20, 30},
				bitmaps: []*roaring64.Bitmap{bitmapOf(0xCC), bitmapOf(0xAA)},
			},
		}

		Convey("returns TokenIDs that contain the exact affinity value", func() {
			ids := idx.ReverseLookup(0xAA)
			So(ids, ShouldResemble, []uint64{10, 20, 30})
		})

		Convey("returns empty bitmap for missing affinity values", func() {
			ids := idx.ReverseLookup(0xDEAD)
			So(ids, ShouldBeNil)
		})
	})
}

func TestExactLookup(t *testing.T) {
	Convey("ExactLookup", t, func() {
		idx := NewSpatialIndex()

		idx.levels = []*lsmLevel{
			{
				keys:    []uint64{10, 20},
				bitmaps: []*roaring64.Bitmap{bitmapOf(0b0000), bitmapOf(0b0001)},
			},
			{
				keys:    []uint64{20, 30},
				bitmaps: []*roaring64.Bitmap{bitmapOf(0b0101), bitmapOf(0b0010)},
			},
		}

		Convey("returns all affinities stored for the exact TokenID", func() {
			bm := idx.ExactLookup(20)
			So(bm.Contains(0b0001), ShouldBeTrue)
			So(bm.Contains(0b0101), ShouldBeTrue)
			So(bm.Contains(0b0000), ShouldBeFalse)
			So(bm.Contains(0b0010), ShouldBeFalse)
		})

		Convey("missing TokenID returns empty bitmap", func() {
			bm := idx.ExactLookup(0xDEAD)
			So(bm.GetCardinality(), ShouldEqual, 0)
		})

		Convey("Or's affinity bitmaps from the same TokenID across levels", func() {
			bm := idx.ExactLookup(20)
			So(bm.GetCardinality(), ShouldEqual, 2)
		})
	})
}

func TestIntersect(t *testing.T) {
	Convey("Intersect", t, func() {
		idx := NewSpatialIndex()

		idx.levels = []*lsmLevel{
			{
				keys:    []uint64{0xA, 0xB},
				bitmaps: []*roaring64.Bitmap{bitmapOf(1, 2, 3), bitmapOf(2, 3, 4)},
			},
		}

		Convey("returns only affinity masks shared by both TokenIDs", func() {
			bm := idx.Intersect(0xA, 0xB)
			So(bm.Contains(1), ShouldBeFalse)
			So(bm.Contains(2), ShouldBeTrue)
			So(bm.Contains(3), ShouldBeTrue)
			So(bm.Contains(4), ShouldBeFalse)
			So(bm.GetCardinality(), ShouldEqual, 2)
		})

		Convey("disjoint TokenIDs return empty bitmap", func() {
			bm := idx.Intersect(0xA, 0xDEAD)
			So(bm.GetCardinality(), ShouldEqual, 0)
		})
	})
}

func TestGetStats(t *testing.T) {
	Convey("GetStats", t, func() {
		idx := NewSpatialIndex()

		idx.levels = []*lsmLevel{
			{
				keys:    []uint64{1, 2},
				bitmaps: []*roaring64.Bitmap{bitmapOf(0b0000), bitmapOf(0b0001)},
			},
			nil, // gap (nil level from LSM cascade)
			{
				keys:    []uint64{3},
				bitmaps: []*roaring64.Bitmap{bitmapOf(0b0010)},
			},
		}

		stats := idx.GetStats()
		So(stats["num_levels"], ShouldEqual, 2) // skips nil
		So(stats["total_affinities"], ShouldEqual, uint64(3))
		So(stats["memory_bytes"], ShouldBeGreaterThan, uint64(0))
	})
}

// --- Benchmarks ---

func BenchmarkInsertBatch(b *testing.B) {
	const batchSize = 10000
	tokenIDs := make([]uint64, batchSize)
	affinities := make([]uint64, batchSize)
	rng := rand.New(rand.NewSource(42))
	for i := range tokenIDs {
		tokenIDs[i] = uint64(i)
		affinities[i] = rng.Uint64()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := NewSpatialIndex()
		idx.InsertBatch(tokenIDs, affinities)
	}
}

func BenchmarkInsertBatchCascade(b *testing.B) {
	// Measures the LSM cascade: many small batches forcing merges.
	const batchSize = 100
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := NewSpatialIndex()
		for j := 0; j < 100; j++ {
			tokenIDs := make([]uint64, batchSize)
			affinities := make([]uint64, batchSize)
			for k := range tokenIDs {
				tokenIDs[k] = uint64(j*batchSize + k)
				affinities[k] = rng.Uint64() & 0xFF // narrow affinity space → collisions
			}
			idx.InsertBatch(tokenIDs, affinities)
		}
	}
}

func BenchmarkQueryHamming(b *testing.B) {
	idx := buildBenchIndex(100000)
	target := uint64(0xAAAAAAAAAAAAAAAA)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.QueryHamming(target, 8)
	}
}

func BenchmarkExactLookup(b *testing.B) {
	idx := buildBenchIndex(100000)
	// Pick an affinity we know exists.
	target := idx.levels[0].keys[0]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.ExactLookup(target)
	}
}

func BenchmarkIntersect(b *testing.B) {
	idx := buildBenchIndex(100000)
	a := idx.levels[0].keys[0]
	last := idx.levels[0].keys[len(idx.levels[0].keys)-1]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Intersect(a, last)
	}
}

// --- Helpers ---

func bitmapOf(ids ...uint64) *roaring64.Bitmap {
	bm := roaring64.New()
	bm.AddMany(ids)
	return bm
}

func buildBenchIndex(n int) *SpatialIndex {
	rng := rand.New(rand.NewSource(99))
	tokenIDs := make([]uint64, n)
	affinities := make([]uint64, n)
	for i := range tokenIDs {
		tokenIDs[i] = uint64(i)
		affinities[i] = rng.Uint64()
	}
	idx := NewSpatialIndex()
	idx.InsertBatch(tokenIDs, affinities)
	return idx
}
