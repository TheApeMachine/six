package lsm

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

func TestLookupByPhaseFollowsResetAwareContinuation(t *testing.T) {
	gc.Convey("Given a compressed reset path in the spatial index", t, func() {
		idx := NewSpatialIndexServer()
		calc := numeric.NewCalculus()

		aPhase := calc.Multiply(1, calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32('a')))
		abPhase := calc.Multiply(aPhase, calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32('b')))
		abaPhase := calc.Multiply(abPhase, calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32('a')))

		idx.insertSync(morton.Pack(0, 'a'), observableValue('a', aPhase, data.OpcodeNext, 'b'), data.MustNewChord())
		idx.insertSync(morton.Pack(1, 'b'), observableValue('b', abPhase, data.OpcodeReset, 'a'), data.MustNewChord())
		idx.insertSync(morton.Pack(0, 'a'), observableValue('a', abaPhase, data.OpcodeHalt, 0), data.MustNewChord())

		gc.Convey("LookupByPhase should return the continuation beyond the exact prompt", func() {
			results, paths, metaPaths := idx.LookupByPhase([]byte("ab"))
			gc.So(len(results), gc.ShouldBeGreaterThan, 0)
			gc.So(len(paths), gc.ShouldEqual, len(results))
			gc.So(len(metaPaths), gc.ShouldEqual, len(paths))
			gc.So(string(results[0]), gc.ShouldEqual, "a")
			gc.So(len(paths[0]), gc.ShouldEqual, 1)

			decoded := idx.decodeChords(paths[0])
			gc.So(len(decoded), gc.ShouldBeGreaterThan, 0)
			gc.So(string(decoded[0]), gc.ShouldEqual, "a")
		})
	})
}

func TestCompactRespectsResetContinuationProgram(t *testing.T) {
	gc.Convey("Given reset-program branches sharing the same compressed radix cell", t, func() {
		idx := NewSpatialIndexServer()
		calc := numeric.NewCalculus()

		aPhase := calc.Multiply(1, calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32('a')))
		abPhase := calc.Multiply(aPhase, calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32('b')))
		abaPhase := calc.Multiply(abPhase, calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32('a')))

		keyA := morton.Pack(0, 'a')
		keyB := morton.Pack(1, 'b')

		idx.insertSync(keyA, observableValue('a', aPhase, data.OpcodeNext, 'b'), data.MustNewChord())
		idx.insertSync(keyB, observableValue('b', abPhase, data.OpcodeReset, 'a'), data.MustNewChord())
		idx.insertSync(keyA, observableValue('a', abaPhase, data.OpcodeHalt, 0), data.MustNewChord())
		idx.insertSync(keyB, observableValue('b', numeric.Phase(99), data.OpcodeReset, 'a'), data.MustNewChord())

		gc.Convey("Compaction should preserve only the branch whose reset continuation really exists", func() {
			idx.mu.Lock()
			pre := idx.followChainUnsafe(keyB)
			idx.mu.Unlock()
			gc.So(len(pre), gc.ShouldEqual, 2)

			pruned := idx.Compact()
			gc.So(pruned, gc.ShouldEqual, 1)

			idx.mu.Lock()
			post := idx.followChainUnsafe(keyB)
			idx.mu.Unlock()
			gc.So(len(post), gc.ShouldEqual, 1)
			gc.So(statePhaseMatches(post[0], 'b', abPhase), gc.ShouldBeTrue)
		})
	})
}
