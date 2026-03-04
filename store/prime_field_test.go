package store

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
)

func TestNewPrimeField(t *testing.T) {
	Convey("Given a NewPrimeField constructor", t, func() {
		Convey("When creating a new instance", func() {
			pf := NewPrimeField()
			So(pf, ShouldNotBeNil)
			So(pf.N, ShouldEqual, 0)
			So(len(pf.chords), ShouldEqual, 0)
			So(len(pf.keys), ShouldEqual, 0)
			So(len(pf.buf), ShouldEqual, 0)
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
				chord[0] = uint64(i + 1)
				pf.Insert(chord, uint64(1000+i))
			}

			So(pf.N, ShouldEqual, numItems)

			// Verify multi-chord aggregation
			So(len(pf.keys), ShouldEqual, numItems)

			for i := 50; i < 150; i++ {
				So(pf.Key(i), ShouldEqual, uint64(1000+i))

				multi := pf.MultiChord(i)
				// The buffer automatically creates OR-aggregations of up to 21 past chords
				So(multi[0][0], ShouldNotEqual, 0)
				So(multi[4][0], ShouldNotEqual, 0)
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
			pf.Insert(data.Chord{}, 42)
			ptr := pf.Field()
			So(ptr, ShouldNotBeNil)

			// Verify it points to the first multichord
			chordsPtr := unsafe.Pointer(&pf.chords[0])
			So(ptr, ShouldEqual, chordsPtr)
		})
	})
}

func TestPrimeFieldMaskUnmaskExhaustive(t *testing.T) {
	Convey("Given a populated PrimeField", t, func() {
		pf := NewPrimeField()

		for i := range 1000 {
			chord := data.Chord{}
			chord[0] = uint64(i + 1)
			pf.Insert(chord, uint64(i))
		}

		Convey("When masking and unmasking numerous indices", func() {
			// Let's test picking random-like spots
			indicesToTest := []int{0, 10, 500, 999}

			for _, idx := range indicesToTest {
				original := pf.Mask(idx)

				// The internal chord should be zeroed out
				masked := pf.MultiChord(idx)
				for j := 0; j < 5; j++ {
					So(masked[j][0], ShouldEqual, 0)
				}

				// Unmask it
				pf.Unmask(idx, original)
				unmasked := pf.MultiChord(idx)
				So(unmasked[0][0], ShouldEqual, original[0][0])
			}
		})
	})
}
