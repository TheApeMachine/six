package store

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

func TestNewPrimeField(t *testing.T) {
	Convey("Given a NewPrimeField constructor", t, func() {
		Convey("When creating a new instance", func() {
			pf := NewPrimeField()
			So(pf, ShouldNotBeNil)
			So(pf.N, ShouldEqual, 0)
			So(len(pf.manifolds), ShouldEqual, 0)
			So(pf.eigen, ShouldNotBeNil)
		})
	})
}

func TestPrimeFieldInsertExhaustive(t *testing.T) {
	Convey("Given an empty PrimeField", t, func() {
		pf := NewPrimeField()

		Convey("When inserting a large sequence of chords", func() {
			numItems := 10000

			for i := range numItems {
				chord := data.Chord{}
				chord.Set(i + 1)
				pf.Insert(chord)
			}

			So(pf.N, ShouldEqual, numItems)

			// Verify topological accumulation mapping
			for i := 50; i < 150; i++ {
				manifold := pf.Manifold(i)
				chord := data.Chord{}
				chord.Set(i + 1)

				cubeIdx, blockIdx := ChordPortalIndices(chord)

				// The buffer dynamically populates the mapped portal block
				So(manifold.Cubes[cubeIdx][blockIdx].Bytes()[0], ShouldNotEqual, 0)
			}
		})
	})
}

func TestPrimeFieldField(t *testing.T) {
	Convey("Given a PrimeField", t, func() {
		pf := NewPrimeField()

		Convey("When calling Field on an empty field", func() {
			ptr := pf.Field()
			So(ptr == nil, ShouldBeTrue)
		})

		Convey("When calling Field on a populated field", func() {
			pf.Insert(data.Chord{})
			ptr := pf.Field()
			So(ptr, ShouldNotBeNil)

			// Verify it points to the first manifold
			manifoldsPtr := unsafe.Pointer(&pf.manifolds[0])
			So(ptr, ShouldEqual, manifoldsPtr)
		})
	})
}

func TestPrimeFieldMaskUnmaskExhaustive(t *testing.T) {
	Convey("Given a populated PrimeField", t, func() {
		pf := NewPrimeField()

		for i := range 1000 {
			chord := data.Chord{}
			chord.Set(i + 1)
			pf.Insert(chord)
		}

		Convey("When masking and unmasking numerous indices", func() {
			// Let's test picking random-like spots
			indicesToTest := []int{0, 10, 500, 999}

			for _, idx := range indicesToTest {
				original := pf.Mask(idx)

				// The internal manifold should be completely zeroed out
				masked := pf.Manifold(idx)
				So(masked, ShouldResemble, geometry.IcosahedralManifold{})

				// Unmask it
				pf.Unmask(idx, original)
				unmasked := pf.Manifold(idx)
				So(unmasked, ShouldResemble, original)
			}
		})
	})
}
