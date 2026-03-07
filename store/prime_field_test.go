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

			// Self-addressing: byte value IS the face index.
			So(frozen.Cubes[cubeFromEvents(nil)][int('a')].ActiveCount(), ShouldBeGreaterThan, 0)
			So(active.Cubes[cubeFromEvents(nil)][int('c')].ActiveCount(), ShouldBeGreaterThan, 0)

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

func TestPrimeFieldInsertTracksHeaderRotationState(t *testing.T) {
	Convey("Given a PrimeField ingesting eventful chords", t, func() {
		pf := NewPrimeField()
		events := []int{
			geometry.EventDensitySpike,
			geometry.EventPhaseInversion,
			geometry.EventDensityTrough,
			geometry.EventLowVarianceFlux,
		}

		expectedRot := uint8(0)
		for i, ev := range events {
			chord := data.Chord{}
			chord.Set((i*17 + 3) % 512)
			pf.Insert(byte(i), uint32(i+1), chord, []int{ev})
			expectedRot = geometry.StateTransitionMatrix[expectedRot][ev]
		}

		Convey("Then active manifold header rotation tracks the same transition matrix", func() {
			active := pf.Manifold(0)
			So(active.Header.RotState(), ShouldEqual, expectedRot)
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
		pf.insertsSinceDensityCheck = 63
		chord := data.Chord{}
		for k := 0; k < 256; k++ {
			chord.Set(k)
		}
		pf.Insert(255, 1, chord, nil)

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
		pf.insertsSinceDensityCheck = 63
		first := data.Chord{}
		first.Set(13)
		pf.Insert(0, 1, first, nil)

		pf.insertsSinceDensityCheck = 63
		second := data.Chord{}
		second.Set(17)
		pf.Insert(0, 2, second, nil)

		pf.insertsSinceDensityCheck = 63
		third := data.Chord{}
		third.Set(19)
		pf.Insert(0, 3, third, nil)

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
			pf.insertsSinceDensityCheck = 63
			chord := data.Chord{}
			chord.Set((i + 1) * 23)
			pf.Insert(0, uint32(i+1), chord, nil)
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
		pf.insertsSinceDensityCheck = 63
		chord := data.Chord{}
		chord.Set(5)
		pf.Insert(0, 2, chord, nil)

		Convey("Then de-mitosis does not trigger prematurely", func() {
			active := pf.Manifold(0)
			So(active.Header.State(), ShouldEqual, 1)
			So(active.Header.Winding(), ShouldBeGreaterThanOrEqualTo, 2)
		})
	})
}
