package lsm

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

func TestLSMCompaction(t *testing.T) {
	Convey("Given a spatial index with a mixture of valid paths and entropy", t, func() {
		spatial := NewSpatialIndexServer()
		calc := numeric.NewCalculus()

		morton := data.NewMortonCoder()

		makeState := func(state int) data.Chord {
			c := data.MustNewChord()
			c.Set(state)
			return c
		}

		mockMeta := data.MustNewChord()

		S1 := numeric.Phase(10)
		S2 := calc.Multiply(S1, calc.Power(3, uint32('B')))
		S3 := calc.Multiply(S2, calc.Power(3, uint32('C')))

		keyA := morton.Pack(0, 'A')
		keyB := morton.Pack(1, 'B')
		keyC := morton.Pack(2, 'C')

		spatial.insertSync(keyA, makeState(int(S1)), mockMeta)
		spatial.insertSync(keyB, makeState(int(S2)), mockMeta)
		spatial.insertSync(keyC, makeState(int(S3)), mockMeta)

		Convey("It should correctly preserve terminal nodes", func() {
			// Without any collisions, nothing should be pruned
			// Terminal node C has no continuation but shouldn't be pruned
			pruned := spatial.Compact()
			So(pruned, ShouldEqual, 0)

			// Chain length for B should be 1
			spatial.mu.Lock()
			chain := spatial.followChainUnsafe(keyB)
			spatial.mu.Unlock()
			So(len(chain), ShouldEqual, 1)
		})

		Convey("It should prune only the destructive interference (phantom branches)", func() {
			// Inject noise at position 1, byte 'B'
			// An incoming state of E1 that has no continuation at position 2
			E1 := 99
			spatial.insertSync(keyB, makeState(E1), mockMeta)

			// Chain length for B should now be 2
			spatial.mu.Lock()
			chainPre := spatial.followChainUnsafe(keyB)
			spatial.mu.Unlock()
			So(len(chainPre), ShouldEqual, 2)

			// Compaction should find that E1 has no valid jump to pos 2
			pruned := spatial.Compact()
			So(pruned, ShouldEqual, 1)

			// Chain length for B should be back to 1
			spatial.mu.Lock()
			chainPost := spatial.followChainUnsafe(keyB)
			spatial.mu.Unlock()
			So(len(chainPost), ShouldEqual, 1)

			// The surviving state should be the valid one (S2)
			So(chainPost[0].Has(int(S2)), ShouldBeTrue)
		})

		Convey("It should prune deeply saturated keys correctly", func() {
			// Inject multiple dead paths
			spatial.insertSync(keyB, makeState(40), mockMeta)
			spatial.insertSync(keyB, makeState(60), mockMeta)
			spatial.insertSync(keyB, makeState(80), mockMeta)

			spatial.mu.Lock()
			chainPre := spatial.followChainUnsafe(keyB)
			spatial.mu.Unlock()
			So(len(chainPre), ShouldEqual, 4)

			pruned := spatial.Compact()
			So(pruned, ShouldEqual, 3)

			spatial.mu.Lock()
			chainPost := spatial.followChainUnsafe(keyB)
			spatial.mu.Unlock()
			So(len(chainPost), ShouldEqual, 1)
			So(chainPost[0].Has(int(S2)), ShouldBeTrue)
		})

		Convey("It should preserve multiple valid paths intersecting at a unified key", func() {
			// Path 2: 'X' -> 'B' -> 'Y'
			// It collides at 'B' (pos 1), but branches off to 'Y' (pos 2)

			S1_X := numeric.Phase(50)
			S2_B := calc.Multiply(S1_X, calc.Power(3, uint32('B')))
			S3_Y := calc.Multiply(S2_B, calc.Power(3, uint32('Y')))

			keyX := morton.Pack(0, 'X')
			keyY := morton.Pack(2, 'Y')

			spatial.insertSync(keyX, makeState(int(S1_X)), mockMeta)
			spatial.insertSync(keyB, makeState(int(S2_B)), mockMeta)
			spatial.insertSync(keyY, makeState(int(S3_Y)), mockMeta)

			spatial.mu.Lock()
			chainPre := spatial.followChainUnsafe(keyB)
			spatial.mu.Unlock()
			So(len(chainPre), ShouldEqual, 2)

			// Compaction should find BOTH states have valid continuations
			// (S2 goes to C, S2_B goes to Y)
			pruned := spatial.Compact()
			So(pruned, ShouldEqual, 0) // Nothing pruned, both valid

			spatial.mu.Lock()
			chainPost := spatial.followChainUnsafe(keyB)
			spatial.mu.Unlock()
			So(len(chainPost), ShouldEqual, 2)
		})
	})
}

