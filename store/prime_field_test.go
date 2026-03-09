package store

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/geometry"
)

func forceNextDensityCheck(pf *PrimeField) {
	pf.insertsSinceDensityCheck = 63
}

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
				pf.Insert(chord)
			}

			So(pf.N, ShouldEqual, 1)

			// Verify topological accumulation mapping on the single manifold
			manifold := pf.Manifold(0)

			bitIdx := (1 + 1) % 512
			var found bool
			for cubeIdx := range 5 {
				for blockIdx := range geometry.CubeFaces {
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

/*
func TestPrimeFieldFreezeBoundaryCreatesFrozenBank(t *testing.T) {
	Convey("Given a PrimeField", t, func() {
		pf := NewPrimeField()
		So(pf.N, ShouldEqual, 1)

		Convey("When a boundary token is inserted", func() {
			pf.Insert(data.BaseChord(123))

			Convey("Then the current working bank is marked frozen", func() {
				// The previous architecture used Insert to freeze, but in the
				// new purely topological view, freeze logic happens externally.
				// This test is kept conceptually in comments for reference to
				// the old N++ behavior.
			})
		})
	})
}
*/

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
			pf.Insert(chord)
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

/*
func TestPrimeFieldInsertPrefersLatestSupportOnConflict(t *testing.T) {
	Convey("Given a PrimeField support block receiving conflicting observations", t, func() {
		pf := NewPrimeField()

		var first data.Chord
		first.Set(11)
		first.Set(17)

		var second data.Chord
		second.Set(29)
		second.Set(31)

		pf.Insert(first)
		pf.Insert(second)

		active := pf.Manifold(0)
		support := active.Cubes[cubeFromEvents(nil)][42]
		veto := active.Cubes[vetoCubeFromSupport(cubeFromEvents(nil))][42]

		Convey("Then the latest chord replaces stale support and stale bits move to veto", func() {
			So(support, ShouldResemble, second)
			So(data.ChordSimilarity(&veto, &first), ShouldEqual, first.ActiveCount())
		})
	})
}

func TestPrimeFieldInsertTriggersMitosisAtDensityThreshold(t *testing.T) {
	Convey("Given a PrimeField accumulating dense support evidence", t, func() {
		pf := NewPrimeField()

		// Pre-fill cube[0] to just under 45% density using bits within [0,511].
		// We set full 512-bit blocks to maximize density per block.
		total := float64(geometry.TotalBitsPerCube)
		target := int(total*0.449) + 1
		for block := 0; block < geometry.CubeFaces && target > 0; block++ {
			for bit := 0; bit < 512 && target > 0; bit++ {
				pf.manifolds[0].Cubes[0][block].Set(bit)
				target--
			}
		}

		// Verify we're just under the threshold.
		So(pf.manifolds[0].ConditionMitosis(), ShouldBeFalse)

		// Insert a dense chord to push density over 45%.
		// byteVal=255 → face 255, which is not fully saturated by the pre-fill
		// (the pre-fill filled 115 full blocks + partial block 116).
		// Force the density check to fire on this insert (stride counter at 63).
		forceNextDensityCheck(pf)
		chord := data.Chord{}
		for k := 0; k < 256; k++ {
			chord.Set(k)
		}
		pf.Insert(chord)

		Convey("Then the active manifold flips into mitosis state at least once", func() {
			So(pf.Manifold(0).Header.State(), ShouldEqual, uint8(1))
		})
	})
}

func TestPrimeFieldRotateIncrementsWindingInMitosis(t *testing.T) {
	Convey("Given a PrimeField already in mitosis mode", t, func() {
		pf := NewPrimeField()
		pf.manifolds[0].Header.SetState(1)

		pf.Rotate([]int{geometry.EventDensitySpike, geometry.EventPhaseInversion})

		Convey("Then each event increments winding", func() {
			active := pf.Manifold(0)
			So(active.Header.Winding(), ShouldEqual, 2)
		})
	})
}

func TestPrimeFieldInsertDeMitosisResetsHeaderAndOrthogonalCubes(t *testing.T) {
	Convey("Given a sparse mitosed manifold with nonzero winding", t, func() {
		pf := NewPrimeField()
		pf.manifolds[0].Header.SetState(1)
		pf.manifolds[0].Header.SetRotState(59)
		pf.manifolds[0].Header.IncrementWinding()
		pf.manifolds[0].Header.IncrementWinding()
		pf.manifolds[0].Header.IncrementWinding()

		pf.manifolds[0].Cubes[1][0].Set(3)
		pf.manifolds[0].Cubes[4][256].Set(7)

		seed := data.Chord{}
		seed.Set(11)
		pf.manifolds[0].Cubes[0][0] = seed

		// Force density check to fire on each insert.
		forceNextDensityCheck(pf)
		first := data.Chord{}
		first.Set(13)
		pf.Insert(first)

		forceNextDensityCheck(pf)
		second := data.Chord{}
		second.Set(17)
		pf.Insert(second)

		forceNextDensityCheck(pf)
		third := data.Chord{}
		third.Set(19)
		pf.Insert(third)

		Convey("Then de-mitosis restores cubic state invariants", func() {
			active := pf.Manifold(0)
			So(active.Header.State(), ShouldEqual, 0)
			So(active.Header.RotState(), ShouldEqual, 11)
			So(active.Header.Winding(), ShouldEqual, 0)
			So(active.Cubes[1][0].ActiveCount(), ShouldEqual, 0)
			So(active.Cubes[4][256].ActiveCount(), ShouldEqual, 0)
		})
	})
}

func TestPrimeFieldInsertRequiresSustainedSparseStreakForDeMitosis(t *testing.T) {
	Convey("Given a sparse mitosed manifold with nonzero winding", t, func() {
		pf := NewPrimeField()
		pf.manifolds[0].Header.SetState(1)
		pf.manifolds[0].Header.IncrementWinding()
		pf.manifolds[0].Header.IncrementWinding()
		pf.manifolds[0].Header.IncrementWinding()

		// Force density check to fire on each insert.
		for i := 0; i < 2; i++ {
			forceNextDensityCheck(pf)
			chord := data.Chord{}
			chord.Set((i + 1) * 23)
			pf.Insert(chord)
		}

		Convey("Then de-mitosis does not trigger before the sparse streak threshold", func() {
			active := pf.Manifold(0)
			So(active.Header.State(), ShouldEqual, 1)
		})
	})
}

func TestPrimeFieldInsertKeepsMitosisWhenAnchorCubeStillDense(t *testing.T) {
	Convey("Given a mitosed manifold with dense anchor cube", t, func() {
		pf := NewPrimeField()
		pf.manifolds[0].Header.SetState(1)
		pf.manifolds[0].Header.IncrementWinding()
		pf.manifolds[0].Header.IncrementWinding()

		bitsToSet := geometry.TotalBitsPerCube * 3 / 10
		for block := 0; block < geometry.CubeFaces && bitsToSet > 0; block++ {
			for bit := 0; bit < 512 && bitsToSet > 0; bit++ {
				pf.manifolds[0].Cubes[0][block].Set(bit)
				bitsToSet--
			}
		}

		// Force density check to fire.
		forceNextDensityCheck(pf)
		chord := data.Chord{}
		chord.Set(5)
		pf.Insert(chord)

		Convey("Then de-mitosis does not trigger prematurely", func() {
			active := pf.Manifold(0)
			So(active.Header.State(), ShouldEqual, 1)
			So(active.Header.Winding(), ShouldBeGreaterThanOrEqualTo, 2)
		})
	})
}
*/
