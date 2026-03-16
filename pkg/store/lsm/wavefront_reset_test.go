package lsm

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

func observableValue(symbol byte, phase numeric.Phase, opcode data.Opcode, next byte) data.Value {
	value := data.NeutralValue()
	value.SetStatePhase(phase)
	if opcode != data.OpcodeHalt {
		value.SetLexicalTransition(next)
	}

	switch opcode {
	case data.OpcodeReset:
		value.SetProgram(data.OpcodeReset, 0, 1, false)
	case data.OpcodeHalt:
		value.SetProgram(data.OpcodeHalt, 0, 0, true)
	default:
		value.SetProgram(data.OpcodeNext, 1, 0, false)
	}

	return data.SeedObservable(symbol, value)
}

func TestWavefrontSearchPromptCanRevisitCompressedResetCell(t *testing.T) {
	gc.Convey("Given a boundary-reset path that reuses the same compressed radix cell", t, func() {
		idx := NewSpatialIndexServer()
		calc := numeric.NewCalculus()

		aPhase := calc.Multiply(1, calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32('a')))
		abPhase := calc.Multiply(aPhase, calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32('b')))
		abaPhase := calc.Multiply(abPhase, calc.Power(numeric.Phase(numeric.FermatPrimitive), uint32('a')))

		idx.insertSync(morton.Pack(0, 'a'), observableValue('a', aPhase, data.OpcodeNext, 'b'), data.MustNewValue())
		idx.insertSync(morton.Pack(1, 'b'), observableValue('b', abPhase, data.OpcodeReset, 'a'), data.MustNewValue())
		idx.insertSync(morton.Pack(0, 'a'), observableValue('a', abaPhase, data.OpcodeHalt, 0), data.MustNewValue())

		wf := NewWavefront(idx, WavefrontWithMaxHeads(32), WavefrontWithMaxDepth(8))

		gc.Convey("SearchPrompt should traverse through reset and decode the repeated cell", func() {
			results := wf.SearchPrompt([]byte("aba"), nil, nil)
			gc.So(len(results), gc.ShouldBeGreaterThan, 0)

			decoded := idx.decodeValues(results[0].Path)
			gc.So(len(decoded), gc.ShouldBeGreaterThan, 0)
			gc.So(string(decoded[0]), gc.ShouldEqual, "aba")
		})
	})
}
