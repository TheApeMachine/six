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
			So(pf.N, ShouldEqual, 1)
			So(len(pf.manifolds), ShouldEqual, 1)
			So(pf.eigen, ShouldNotBeNil)
		})
	})
}

func TestPrimeFieldInsertExhaustive(t *testing.T) {
	Convey("Given an empty PrimeField", t, func() {
		pf := NewPrimeField()

		Convey("When inserting a large sequence of chords", func() {
			numItems := 1000

			// Simulate distinct topological chords being ingested.
			// It should continuously map across the 5x27 geometry and trigger
			// entropy rotations on dense blocks instead of scaling the array length.
			for i := range numItems {
				chord := data.Chord{}
				chord.Set((i + 1) % 512)
				pf.Insert(byte(i), uint32(i), chord, []int{})
			}

			So(pf.N, ShouldEqual, 1)

			// Verify topological accumulation mapping on the single manifold
			manifold := pf.Manifold(0)

			bitIdx := (1 + 1) % 512
			var found bool
			for cubeIdx := range 5 {
				for blockIdx := range 27 {
					if manifold.Cubes[cubeIdx][blockIdx][bitIdx/64]&(uint64(1)<<uint(bitIdx%64)) != 0 {
						found = true
						break
					}
				}
				if found {
					break
				}
			}

			So(found, ShouldBeTrue)
		})
	})
}

func TestPrimeFieldFreezeBoundaryCreatesFrozenBank(t *testing.T) {
	Convey("Given a PrimeField with active manifold data", t, func() {
		pf := NewPrimeField()

		chA := data.BaseChord('a')
		chB := data.BaseChord('b')
		chC := data.BaseChord('c')

		pf.Insert('a', 0, chA, []int{})
		pf.Insert('b', 1, chB, []int{})
		pf.Insert('c', 0, chC, []int{})

		Convey("When a new segment boundary arrives", func() {
			So(pf.N, ShouldEqual, 2)
			So(len(pf.manifolds), ShouldEqual, 2)

			frozen := pf.Manifold(1)
			active := pf.Manifold(0)

			So(frozen.Cubes[cubeFromEvents(nil)][blockFromChordDynamics(0, chA, nil)].ActiveCount(), ShouldBeGreaterThan, 0)
			So(active.Cubes[cubeFromEvents(nil)][blockFromChordDynamics(0, chC, nil)].ActiveCount(), ShouldBeGreaterThan, 0)

			ptr, n, offset := pf.SearchSnapshot()
			So(ptr, ShouldNotBeNil)
			So(n, ShouldEqual, 1)
			So(offset, ShouldEqual, 1)
		})
	})
}

func TestPrimeFieldField(t *testing.T) {
	Convey("Given a PrimeField", t, func() {
		pf := NewPrimeField()

		Convey("When calling Field on an allocated field", func() {
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

		for i := range 10 {
			chord := data.Chord{}
			chord.Set((i + 1) % 512)
			pf.Insert(byte(i), uint32(i), chord, []int{})
		}

		Convey("When masking and unmasking index 0", func() {
			original := pf.Mask(0)

			// The internal manifold should be completely zeroed out
			masked := pf.Manifold(0)
			So(masked, ShouldResemble, geometry.IcosahedralManifold{})

			// Unmask it
			pf.Unmask(0, original)
			unmasked := pf.Manifold(0)
			So(unmasked, ShouldResemble, original)
		})
	})
}
