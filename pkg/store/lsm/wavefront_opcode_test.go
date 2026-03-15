package lsm

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

func TestWavefrontRespectsJumpOpcodes(t *testing.T) {
	gc.Convey("Given a sparse index with an explicit jump chord", t, func() {
		idx := NewSpatialIndexServer()
		calc := numeric.NewCalculus()

		stateA := calc.Multiply(1, calc.Power(3, uint32('A')))
		chordA := data.BaseChord('A')
		chordA.Set(int(stateA))
		chordA.SetProgram(data.OpcodeJump, 2, 0, false)
		idx.insertSync(morton.Pack(0, 'A'), chordA, data.MustNewChord())

		stateC := calc.Multiply(stateA, calc.Power(3, uint32('C')))
		chordC := data.BaseChord('C')
		chordC.Set(int(stateC))
		chordC.SetProgram(data.OpcodeHalt, 0, 0, true)
		idx.insertSync(morton.Pack(2, 'C'), chordC, data.MustNewChord())

		wf := NewWavefront(idx, WavefrontWithMaxDepth(4))
		results := wf.Search(data.BaseChord('A'), nil, nil)

		gc.Convey("The head should advance directly to the jumped position", func() {
			gc.So(len(results), gc.ShouldBeGreaterThan, 0)
			gc.So(len(results[0].Path), gc.ShouldEqual, 2)
			gc.So(results[0].Depth, gc.ShouldEqual, uint32(2))
			gc.So(results[0].Phase, gc.ShouldEqual, stateC)
		})
	})
}
