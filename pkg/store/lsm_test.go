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
		Convey("colliding TokenIDs merge their value stores", func() {
			va := makeValue(1)
			vb := makeValue(2)

			sa := newValueStore()
			sa.add(va)
			sb := newValueStore()
			sb.add(vb)

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
			So(len(merged.stores[0].frames), ShouldEqual, 2)
		})

		Convey("disjoint TokenIDs stay separate and sorted", func() {
			sa := newValueStore()
			sa.add(makeValue(10))
			sb := newValueStore()
			sb.add(makeValue(20))

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

		Convey("duplicate TokenIDs within a batch deduplicate", func() {
			idx := NewSpatialIndex()
			v := makeValue(7)
			idx.InsertBatch([]uint64{7, 7, 7}, v)

			bm := idx.ExactLookup(7)
			So(bm.GetCardinality(), ShouldBeGreaterThan, uint64(0))
		})

		Convey("colliding TokenIDs across batches merge bitmaps", func() {
			idx := NewSpatialIndex()
			v1 := makeValue(9)
			v2 := makeValue(99)
			idx.InsertBatch([]uint64{9}, v1)
			idx.InsertBatch([]uint64{9}, v2)

			bm := idx.ExactLookup(9)
			So(bm.GetCardinality(), ShouldBeGreaterThan, uint64(0))
		})
	})
}

func TestQueryHamming(t *testing.T) {
	Convey("QueryHamming", t, func() {
		idx := NewSpatialIndex()

		// Insert a Value whose affinity word (index 63) is 0b0000.
		v0 := makeValue(0)
		v0[63] = 0b0000
		idx.InsertBatch([]uint64{1}, v0)

		// Insert a Value whose affinity word is 0b0001 (distance 1 from 0b0000).
		v1 := makeValue(1)
		v1[63] = 0b0001
		idx.InsertBatch([]uint64{2}, v1)

		Convey("distance 0 returns only exact affinity match", func() {
			frames := idx.QueryHamming(0b0000, 0)
			So(len(frames), ShouldEqual, 1)
			So(frames[0][63], ShouldEqual, uint64(0b0000))
		})

		Convey("distance 1 includes single-bit neighbor", func() {
			frames := idx.QueryHamming(0b0000, 1)
			So(len(frames), ShouldEqual, 2)
		})

		Convey("empty index returns nil", func() {
			empty := NewSpatialIndex()
			frames := empty.QueryHamming(0xDEAD, 64)
			So(frames, ShouldBeNil)
		})
	})
}

func TestExactLookup(t *testing.T) {
	Convey("ExactLookup", t, func() {
		idx := NewSpatialIndex()
		v := makeValue(42)
		idx.InsertBatch([]uint64{10}, v)

		Convey("returns non-empty bitmap for the exact TokenID", func() {
			bm := idx.ExactLookup(10)
			So(bm.GetCardinality(), ShouldBeGreaterThan, uint64(0))
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
	return idx
}
