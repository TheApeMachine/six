package store

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	bsi "github.com/RoaringBitmap/roaring/v2/BitSliceIndexing"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/viper"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

func resolveStoreTestConfigPath() string {
	if envPath := strings.TrimSpace(os.Getenv("TEST_CONFIG_PATH")); envPath != "" {
		return filepath.Clean(envPath)
	}
	_, file, _, ok := runtime.Caller(0)
	if ok {
		return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "cmd", "cfg", "config.yml"))
	}
	return filepath.Clean(filepath.Join("..", "..", "cmd", "cfg", "config.yml"))
}

func TestMain(m *testing.M) {
	viper.SetConfigFile(resolveStoreTestConfigPath())
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "store: viper.ReadInConfig: %v\n", err)
		os.Exit(1)
	}
	core.NewConfig()
	ResetDefaultSpatialIndex()
	os.Exit(m.Run())
}

func makeValue(seed uint64) [valueWords]uint64 {
	var v [valueWords]uint64
	rng := rand.New(rand.NewSource(int64(seed)))
	for i := range v {
		v[i] = rng.Uint64()
	}
	return v
}

func makeValueWithIDAndPC(seed uint64, valueID, pc uint64) [valueWords]uint64 {
	v := makeValue(seed)
	reg := core.Cfg.Value.Region
	v[reg.ID.Start] = valueID
	v[reg.Registers.PC] = pc
	return v
}

func TestMergeLevels(t *testing.T) {
	Convey("mergeLevels", t, func() {
		Convey("colliding TokenIDs merge their postings with OR", func() {
			sa := newValueStore()
			sa.postings.Add(100)
			sa.postings.Add(101)
			sb := newValueStore()
			sb.postings.Add(102)

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
			So(merged.stores[0].postings.GetCardinality(), ShouldEqual, uint64(3))
			So(merged.stores[0].postings.Contains(100), ShouldBeTrue)
			So(merged.stores[0].postings.Contains(101), ShouldBeTrue)
			So(merged.stores[0].postings.Contains(102), ShouldBeTrue)
		})

		Convey("disjoint TokenIDs stay separate and sorted", func() {
			sa := newValueStore()
			sa.postings.Add(1)
			sb := newValueStore()
			sb.postings.Add(2)

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

		pv := primitive.Value(v)

		Convey("returns all keys for that exact frame", func() {
			keys := idx.LookupKeysByValue(&pv)
			So(len(keys), ShouldEqual, 3)
		})

		Convey("empty index returns nil", func() {
			empty := NewSpatialIndex()
			So(empty.LookupKeysByValue(&pv), ShouldBeNil)
		})

		Convey("different frame returns nil", func() {
			other := primitive.Value(makeValue(99))
			So(idx.LookupKeysByValue(&other), ShouldBeNil)
		})
	})
}

func TestLookupKeysByValueID(t *testing.T) {
	Convey("LookupKeysByValueID", t, func() {
		idx := NewSpatialIndex()

		reg := core.Cfg.Value.Region
		v := makeValue(42)
		v[reg.ID.Start] = 4242

		idx.InsertBatch([]uint64{100, 200, 300}, v)

		Convey("returns token keys that post the ValueID", func() {
			keys := idx.LookupKeysByValueID(4242)
			So(len(keys), ShouldEqual, 3)
		})

		Convey("still resolves after the live frame diverges from ingest bitmap", func() {
			live := primitive.Value(v)
			live[0] ^= 0xFFFFFFFFFFFFFFFF

			So(idx.LookupKeysByValue(&live), ShouldBeNil)
			So(len(idx.LookupKeysByValueID(4242)), ShouldEqual, 3)
		})

		Convey("zero ValueID returns nil", func() {
			So(idx.LookupKeysByValueID(0), ShouldBeNil)
		})

		Convey("unknown ValueID returns nil", func() {
			So(idx.LookupKeysByValueID(999999), ShouldBeNil)
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

func TestComparePC(t *testing.T) {
	Convey("ComparePC uses BSI over dense columns", t, func() {
		idx := NewSpatialIndex()
		lowPC := makeValueWithIDAndPC(11, 5000, 0)
		highPC := makeValueWithIDAndPC(12, 5001, 100)
		idx.InsertBatch([]uint64{42}, lowPC)
		idx.InsertBatch([]uint64{42}, highPC)

		candidates := idx.ValueIDsForToken(42)
		So(candidates.GetCardinality(), ShouldEqual, uint64(2))

		lt := idx.ComparePC(0, bsi.LT, 10, 0, candidates)
		So(lt.Contains(5000), ShouldBeTrue)
		So(lt.Contains(5001), ShouldBeFalse)

		eq := idx.ComparePC(0, bsi.EQ, 100, 0, candidates)
		So(eq.Contains(5001), ShouldBeTrue)
		So(eq.Contains(5000), ShouldBeFalse)
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
	pv := primitive.Value(makeValue(99))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.LookupKeysByValue(&pv)
	}
}

func BenchmarkLookupKeysByValueID(b *testing.B) {
	idx := NewSpatialIndex()

	reg := core.Cfg.Value.Region
	fr := makeValue(7)
	fr[reg.ID.Start] = 424242

	idx.InsertBatch([]uint64{0xFFFF_AAAA_0001, 0xFFFF_AAAA_0002, 0xFFFF_AAAA_0003}, fr)

	id := fr[reg.ID.Start]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.LookupKeysByValueID(id)
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
