package lsm

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

func phaseBefore(symbol byte, next numeric.Phase, calc *numeric.Calculus) numeric.Phase {
	invRot, err := calc.Inverse(calc.Power(3, uint32(symbol)))
	if err != nil {
		panic(err)
	}
	return calc.Multiply(next, invRot)
}

func TestWavefrontAnchorSnap(t *testing.T) {
	gc.Convey("Given an anchor chord at a periodic stride", t, func() {
		idx := NewSpatialIndexServer()
		calc := numeric.NewCalculus()

		const pos uint32 = 256
		const symbol byte = 'B'
		anchorPhase := numeric.Phase(200)

		anchorChord := data.BaseChord(symbol)
		anchorChord.Set(int(anchorPhase))
		anchorChord.SetResidualCarry(uint64(anchorPhase))
		anchorChord.SetProgram(data.OpcodeHalt, 0, 0, true)
		idx.insertSync(morton.Pack(pos, symbol), anchorChord, data.MustNewChord())

		jump := data.MustNewChord()
		jump.SetProgram(data.OpcodeJump, pos, 0, false)

		driftedExpected := numeric.Phase(206)
		head := &WavefrontHead{
			phase:   phaseBefore(symbol, driftedExpected, calc),
			pos:     0,
			path:    []data.Chord{jump},
			visited: map[visitMark]bool{},
		}

		wf := NewWavefront(idx, WavefrontWithAnchors(pos, 10))
		next := wf.advance([]*WavefrontHead{head}, data.BaseChord(symbol), nil, nil)

		gc.Convey("The branch should snap onto the stored anchor phase", func() {
			gc.So(len(next), gc.ShouldEqual, 1)
			gc.So(next[0].pos, gc.ShouldEqual, pos)
			gc.So(next[0].phase, gc.ShouldEqual, anchorPhase)
			gc.So(len(next[0].path), gc.ShouldEqual, 2)
		})
	})
}

func TestWavefrontAnchorRejectsWideDrift(t *testing.T) {
	gc.Convey("Given a branch whose phase drifts too far from the anchor", t, func() {
		idx := NewSpatialIndexServer()
		calc := numeric.NewCalculus()

		const pos uint32 = 256
		const symbol byte = 'B'
		anchorPhase := numeric.Phase(200)

		anchorChord := data.BaseChord(symbol)
		anchorChord.Set(int(anchorPhase))
		anchorChord.SetResidualCarry(uint64(anchorPhase))
		anchorChord.SetProgram(data.OpcodeHalt, 0, 0, true)
		idx.insertSync(morton.Pack(pos, symbol), anchorChord, data.MustNewChord())

		jump := data.MustNewChord()
		jump.SetProgram(data.OpcodeJump, pos, 0, false)

		driftedExpected := numeric.Phase(230)
		head := &WavefrontHead{
			phase:   phaseBefore(symbol, driftedExpected, calc),
			pos:     0,
			path:    []data.Chord{jump},
			visited: map[visitMark]bool{},
		}

		wf := NewWavefront(idx, WavefrontWithAnchors(pos, 5))
		next := wf.advance([]*WavefrontHead{head}, data.BaseChord(symbol), nil, nil)

		gc.Convey("The branch should be kept at the old state instead of advancing through the bad anchor", func() {
			gc.So(len(next), gc.ShouldEqual, 1)
			gc.So(next[0].pos, gc.ShouldEqual, uint32(0))
			gc.So(next[0].phase, gc.ShouldEqual, head.phase)
			gc.So(len(next[0].path), gc.ShouldEqual, 1)
		})
	})
}
