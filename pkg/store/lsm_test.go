package store

import (
	"math/rand"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func makeValue(seed uint64) [valueWords]uint64 {
	var v [valueWords]uint64
	rng := rand.New(rand.NewSource(int64(seed)))
	for i := range v {
		v[i] = rng.Uint64()
	}
	return v
}

func TestMergeLevels(t *testing.T) {
	Convey("mergeLevels", t, func() {
		Convey("colliding TokenIDs merge their bitmaps with OR", func() {
			va := makeValue(1)
			vb := makeValue(2)

			sa := newValueStore()
			sa.bitmap.Or(ValueFrameBitmap(va))
			sb := newValueStore()
			sb.bitmap.Or(ValueFrameBitmap(vb))

			a := &lsmLevel{
				keys:   []uint64{0b0001},
				stores: []*valueStore{sa},
			}
			b := &lsmLevel{
				keys:   []uint64{0b0001},
				stores: []*valueStore{sb},
			}

			merged := mergeLevels(a, b)

			So(len(merged.keys), ShouldEqual, 1)
			So(merged.keys[0], ShouldEqual, 0b0001)
			union := ValueFrameBitmap(va)
			tmp := ValueFrameBitmap(vb)
			union.Or(tmp)
			So(merged.stores[0].bitmap.Equals(union), ShouldBeTrue)
		})

		Convey("disjoint TokenIDs stay separate and sorted", func() {
			sa := newValueStore()
			sa.bitmap.Or(ValueFrameBitmap(makeValue(10)))
			sb := newValueStore()
			sb.bitmap.Or(ValueFrameBitmap(makeValue(20)))

			a := &lsmLevel{
				keys:   []uint64{0b0010},
				stores: []*valueStore{sa},
			}
			b := &lsmLevel{
				keys:   []uint64{0b0001},
				stores: []*valueStore{sb},
			}

			merged := mergeLevels(a, b)

			So(len(merged.keys), ShouldEqual, 2)
			So(merged.keys[0], ShouldEqual, 0b0001)
			So(merged.keys[1], ShouldEqual, 0b0010)
		})
	})
}

func TestInsertBatch(t *testing.T) {
	Convey("InsertBatch", t, func() {
		Convey("empty batch is a no-op", func() {
			idx := NewSpatialIndex()
			idx.InsertBatch(nil, makeValue(0))
			So(len(idx.levels), ShouldEqual, 0)
		})

		Convey("single batch lands in level 0 after flush", func() {
			idx := NewSpatialIndex()
			idx.InsertBatch([]uint64{1, 2}, makeValue(1))
			idx.Flush()
			So(len(idx.levels), ShouldEqual, 1)
			So(idx.levels[0], ShouldNotBeNil)
			So(len(idx.levels[0].keys), ShouldEqual, 2)
		})

		Convey("two separate flushes cascade into level 1", func() {
			idx := NewSpatialIndex()
			idx.InsertBatch([]uint64{1}, makeValue(1))
			idx.Flush()
			idx.InsertBatch([]uint64{2}, makeValue(2))
			idx.Flush()

			So(idx.levels[0], ShouldBeNil)
			So(idx.levels[1], ShouldNotBeNil)
		})

		Convey("duplicate TokenIDs within a batch merge bitmap", func() {
			idx := NewSpatialIndex()
			v := makeValue(7)
			idx.InsertBatch([]uint64{7, 7, 7}, v)

			bm := idx.ExactLookup(7)
			So(bm.GetCardinality(), ShouldEqual, ValueFrameBitmap(v).GetCardinality())
		})

		Convey("colliding TokenIDs across batches merge bitmaps", func() {
			idx := NewSpatialIndex()
			v1 := makeValue(9)
			v2 := makeValue(99)
			idx.InsertBatch([]uint64{9}, v1)
			idx.InsertBatch([]uint64{9}, v2)

			bm := idx.ExactLookup(9)
			union := ValueFrameBitmap(v1)
			union.Or(ValueFrameBitmap(v2))
			So(bm.Equals(union), ShouldBeTrue)
		})
	})
}

func TestLookupKeysByValue(t *testing.T) {
	Convey("LookupKeysByValue", t, func() {
		idx := NewSpatialIndex()
		v := makeValue(42)
		idx.InsertBatch([]uint64{100, 200, 300}, v)

		Convey("returns all keys for that exact frame", func() {
			keys := idx.LookupKeysByValue(v)
			So(len(keys), ShouldEqual, 3)
		})

		Convey("empty index returns nil", func() {
			empty := NewSpatialIndex()
			So(empty.LookupKeysByValue(v), ShouldBeNil)
		})

		Convey("different frame returns nil", func() {
			So(idx.LookupKeysByValue(makeValue(99)), ShouldBeNil)
		})
	})
}

func TestExactLookup(t *testing.T) {
	Convey("ExactLookup", t, func() {
		idx := NewSpatialIndex()
		v := makeValue(42)
		idx.InsertBatch([]uint64{10}, v)

		Convey("returns encoded bitmap for the exact TokenID", func() {
			bm := idx.ExactLookup(10)
			So(bm.Equals(ValueFrameBitmap(v)), ShouldBeTrue)
		})

		Convey("missing TokenID returns empty bitmap", func() {
			bm := idx.ExactLookup(0xDEAD)
			So(bm.GetCardinality(), ShouldEqual, uint64(0))
		})
	})
}

func TestGetStats(t *testing.T) {
	Convey("GetStats", t, func() {
		idx := NewSpatialIndex()
		idx.InsertBatch([]uint64{1, 2}, makeValue(1))
		idx.InsertBatch([]uint64{3}, makeValue(2))

		stats := idx.GetStats()
		So(stats["num_levels"], ShouldBeGreaterThan, 0)
		So(stats["memory_bytes"], ShouldBeGreaterThan, uint64(0))
		So(stats["total_keys"], ShouldEqual, uint64(3))
	})
}

// --- Benchmarks ---

func BenchmarkInsertBatch(b *testing.B) {
	const batchSize = 10000
	tokenIDs := make([]uint64, batchSize)
	rng := rand.New(rand.NewSource(42))
	for i := range tokenIDs {
		tokenIDs[i] = uint64(i)
	}
	v := makeValue(rng.Uint64())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := NewSpatialIndex()
		idx.InsertBatch(tokenIDs, v)
	}
}

func BenchmarkInsertBatchCascade(b *testing.B) {
	const batchSize = 100
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := NewSpatialIndex()
		for j := 0; j < 100; j++ {
			tokenIDs := make([]uint64, batchSize)
			for k := range tokenIDs {
				tokenIDs[k] = uint64(j*batchSize + k)
			}
			idx.InsertBatch(tokenIDs, makeValue(rng.Uint64()))
		}
	}
}

func BenchmarkLookupKeysByValue(b *testing.B) {
	idx := buildBenchIndex(100000)
	v := makeValue(99)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.LookupKeysByValue(v)
	}
}

func BenchmarkExactLookup(b *testing.B) {
	idx := buildBenchIndex(100000)
	target := idx.levels[0].keys[0]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.ExactLookup(target)
	}
}

// --- Helpers ---

func buildBenchIndex(n int) *SpatialIndex {
	rng := rand.New(rand.NewSource(99))
	tokenIDs := make([]uint64, n)
	for i := range tokenIDs {
		tokenIDs[i] = uint64(i)
	}
	idx := NewSpatialIndex()
	idx.InsertBatch(tokenIDs, makeValue(rng.Uint64()))
	idx.Flush()
	return idx
}
