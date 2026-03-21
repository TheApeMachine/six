package topology

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

/*
TestMakeSet verifies that MakeSet creates singleton components with
distinct IDs and increments the component count.
*/
func TestMakeSet(t *testing.T) {
	gc.Convey("Given a fresh UnionFind", t, func() {
		uf := NewUnionFind(16)

		gc.Convey("It should start with zero components", func() {
			gc.So(uf.Components(), gc.ShouldEqual, 0)
		})

		gc.Convey("It should assign sequential IDs and count singletons", func() {
			idA := uf.MakeSet()
			idB := uf.MakeSet()
			idC := uf.MakeSet()

			gc.So(idA, gc.ShouldEqual, 0)
			gc.So(idB, gc.ShouldEqual, 1)
			gc.So(idC, gc.ShouldEqual, 2)
			gc.So(uf.Components(), gc.ShouldEqual, 3)
		})
	})
}

/*
TestFind verifies that Find returns roots and performs path compression.
*/
func TestFind(t *testing.T) {
	gc.Convey("Given singletons", t, func() {
		uf := NewUnionFind(8)
		idA := uf.MakeSet()
		idB := uf.MakeSet()

		gc.Convey("It should find each element as its own root", func() {
			gc.So(uf.Find(idA), gc.ShouldEqual, idA)
			gc.So(uf.Find(idB), gc.ShouldEqual, idB)
		})

		gc.Convey("It should compress paths after unions", func() {
			idC := uf.MakeSet()
			idD := uf.MakeSet()

			uf.Union(idA, idB)
			uf.Union(idC, idD)
			uf.Union(idA, idC)

			rootAll := uf.Find(idD)

			gc.So(uf.Find(idA), gc.ShouldEqual, rootAll)
			gc.So(uf.Find(idB), gc.ShouldEqual, rootAll)
			gc.So(uf.Find(idC), gc.ShouldEqual, rootAll)
		})
	})
}

/*
TestUnion verifies merging distinct vs same components.
*/
func TestUnion(t *testing.T) {
	gc.Convey("Given four singleton components", t, func() {
		uf := NewUnionFind(4)
		idA := uf.MakeSet()
		idB := uf.MakeSet()
		idC := uf.MakeSet()
		_ = uf.MakeSet()

		gc.So(uf.Components(), gc.ShouldEqual, 4)

		gc.Convey("It should return true when merging distinct components", func() {
			gc.So(uf.Union(idA, idB), gc.ShouldBeTrue)
			gc.So(uf.Components(), gc.ShouldEqual, 3)

			gc.So(uf.Union(idB, idC), gc.ShouldBeTrue)
			gc.So(uf.Components(), gc.ShouldEqual, 2)
		})

		gc.Convey("It should return false when merging the same component", func() {
			uf.Union(idA, idB)
			gc.So(uf.Union(idA, idB), gc.ShouldBeFalse)
			gc.So(uf.Components(), gc.ShouldEqual, 3)
		})
	})
}

/*
TestConnected verifies the Connected predicate mirrors Find equality.
*/
func TestConnected(t *testing.T) {
	gc.Convey("Given two singletons merged together", t, func() {
		uf := NewUnionFind(4)
		idA := uf.MakeSet()
		idB := uf.MakeSet()
		idC := uf.MakeSet()

		gc.Convey("It should report disconnected before union", func() {
			gc.So(uf.Connected(idA, idB), gc.ShouldBeFalse)
			gc.So(uf.Connected(idA, idC), gc.ShouldBeFalse)
		})

		gc.Convey("It should report connected after union", func() {
			uf.Union(idA, idB)

			gc.So(uf.Connected(idA, idB), gc.ShouldBeTrue)
			gc.So(uf.Connected(idA, idC), gc.ShouldBeFalse)
		})
	})
}

/*
TestReset verifies that Reset clears all state for reuse.
*/
func TestReset(t *testing.T) {
	gc.Convey("Given a populated UnionFind", t, func() {
		uf := NewUnionFind(8)
		uf.MakeSet()
		uf.MakeSet()
		uf.MakeSet()
		uf.Union(0, 1)

		gc.So(uf.Components(), gc.ShouldEqual, 2)

		gc.Convey("It should clear all state on Reset", func() {
			uf.Reset()

			gc.So(uf.Components(), gc.ShouldEqual, 0)

			newID := uf.MakeSet()
			gc.So(newID, gc.ShouldEqual, 0)
			gc.So(uf.Components(), gc.ShouldEqual, 1)
		})
	})
}

/*
BenchmarkMakeSet measures singleton creation throughput.
*/
func BenchmarkMakeSet(b *testing.B) {
	uf := NewUnionFind(b.N)
	b.ResetTimer()

	for b.Loop() {
		uf.MakeSet()
	}
}

/*
BenchmarkFind measures Find with path halving on a deep chain.
*/
func BenchmarkFind(b *testing.B) {
	const depth = 4096
	uf := NewUnionFind(depth)

	for range depth {
		uf.MakeSet()
	}

	for idx := int32(1); idx < depth; idx++ {
		uf.Union(idx-1, idx)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		uf.Find(depth - 1)
	}
}

/*
BenchmarkUnion measures Union throughput by rebuilding a fresh
structure each iteration and merging all pairs.
*/
func BenchmarkUnion(b *testing.B) {
	const pairCount = 512
	uf := NewUnionFind(pairCount * 2)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		uf.Reset()

		for range pairCount * 2 {
			uf.MakeSet()
		}

		for idx := int32(0); idx < pairCount*2; idx += 2 {
			uf.Union(idx, idx+1)
		}
	}
}
