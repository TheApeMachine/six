package lsm

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

func TestWavefrontRespectsJumpOpcodes(t *testing.T) {
	gc.Convey("Given a sparse index with an explicit jump value", t, func() {
		idx := NewSpatialIndexServer()
		calc := numeric.NewCalculus()

		stateA := calc.Multiply(1, calc.Power(3, uint32('A')))
		valueA := data.BaseValue('A')
		valueA.Set(int(stateA))
		valueA.SetProgram(data.OpcodeJump, 2, 0, false)
		idx.insertSync(morton.Pack(0, 'A'), valueA, data.MustNewValue())

		stateC := calc.Multiply(stateA, calc.Power(3, uint32('C')))
		valueC := data.BaseValue('C')
		valueC.Set(int(stateC))
		valueC.SetProgram(data.OpcodeHalt, 0, 0, true)
		idx.insertSync(morton.Pack(2, 'C'), valueC, data.MustNewValue())

		wf := NewWavefront(idx, WavefrontWithMaxDepth(4))
		results := wf.Search(data.BaseValue('A'), nil, nil)

		gc.Convey("The head should advance directly to the jumped position", func() {
			gc.So(len(results), gc.ShouldBeGreaterThan, 0)
			gc.So(len(results[0].Path), gc.ShouldEqual, 2)
			gc.So(results[0].Depth, gc.ShouldEqual, uint32(2))
			gc.So(results[0].Phase, gc.ShouldEqual, stateC)
		})
	})
}
