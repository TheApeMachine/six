package path

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

func WavefrontValue(
	phase numeric.Phase,
	nextPhase numeric.Phase,
	opcode primitive.Opcode,
) primitive.Value {
	value := primitive.NeutralValue()
	value.SetStatePhase(phase)

	if nextPhase != 0 {
		value.SetTrajectory(phase, nextPhase)
	}

	terminal := opcode == primitive.OpcodeHalt
	jump := uint32(1)
	if terminal {
		jump = 0
	}

	value.SetProgram(opcode, jump, 0, terminal)
	return value
}

func compiledSequence(payload []byte) ([]primitive.Value, []primitive.Value) {
	coder := data.NewMortonCoder()
	keys := make([]uint64, len(payload))

	for index, symbol := range payload {
		keys[index] = coder.Pack(uint32(index), symbol)
	}

	cells := primitive.CompileSequenceCells(keys)
	values := make([]primitive.Value, len(cells))
	metaValues := make([]primitive.Value, len(cells))

	for index, cell := range cells {
		values[index] = primitive.SeedObservable(cell.Symbol, cell.Value)
		metaValues[index] = cell.Meta
	}

	return values, metaValues
}

func TestPathWavefrontAnchorSnap(t *testing.T) {
	state := errnie.NewState("logic/path/wavefront/anchorSnap/test")

	gc.Convey("Given a prefetched path with a drifted transition and a valid anchor", t, func() {
		currentValue := WavefrontValue(10, 206, primitive.OpcodeNext)
		anchorValue := WavefrontValue(200, 0, primitive.OpcodeHalt)
		meta := errnie.Guard(state, func() (primitive.Value, error) {
			return primitive.New()
		})

		paths := [][]primitive.Value{{currentValue, anchorValue}}
		metaPaths := [][]primitive.Value{{meta, meta}}

		gc.Convey("the substrate wavefront should snap the transition onto the anchor", func() {
			wavefront := NewWavefront(WavefrontWithAnchors(1, 10))
			stablePaths, stableMetaPaths, err := wavefront.Stabilize(paths, metaPaths)

			gc.So(err, gc.ShouldBeNil)
			gc.So(len(stablePaths), gc.ShouldEqual, 1)
			gc.So(len(stableMetaPaths), gc.ShouldEqual, 1)
			gc.So(len(stablePaths[0]), gc.ShouldEqual, 2)
			gc.So(wavefront.phaseForValue(stablePaths[0][1]), gc.ShouldEqual, numeric.Phase(200))
		})

		gc.Convey("without anchors the same discontinuity should be rejected exactly", func() {
			wavefront := NewWavefront()
			_, _, err := wavefront.Stabilize(paths, metaPaths)

			gc.So(err, gc.ShouldNotBeNil)
			gc.So(err.Error(), gc.ShouldContainSubstring, ErrWavefrontTransitionRejected.Error())
		})
	})
}

// func TestPathWavefrontBridgeSynthesis(t *testing.T) {
// 	gc.Convey("Given a prefetched path with a genuine phase discontinuity", t, func() {
// 		ctx := context.Background()
// 		currentValue := WavefrontValue(10, 19, data.OpcodeNext)
// 		targetValue := WavefrontValue(200, 0, data.OpcodeHalt)
// 		meta := data.MustNewValue()

// 		key := macro.ComputeExpectedAffineKey(currentValue, targetValue)
// 		macroIndex := macro.NewMacroIndexServer(macro.MacroIndexWithContext(ctx))
// 		for range 6 {
// 			macroIndex.RecordOpcode(key)
// 		}

// 		fe := goal.NewFrustrationEngineServer(
// 			goal.FrustrationWithContext(ctx),
// 			goal.WithSharedIndex(macroIndex),
// 		)
// 		defer fe.Close()

// 		wavefront := NewWavefront(
// 			WavefrontWithFrustrationEngine(fe, int(numeric.FermatPrime), 2),
// 		)

// 		paths := [][]data.Value{{currentValue, targetValue}}
// 		metaPaths := [][]data.Value{{meta, meta}}

// 		gc.Convey("the wavefront should insert an exact synthetic bridge into the graph path", func() {
// 			stablePaths, stableMetaPaths, err := wavefront.Stabilize(paths, metaPaths)

// 			gc.So(err, gc.ShouldBeNil)
// 			gc.So(len(stablePaths), gc.ShouldEqual, 1)
// 			gc.So(len(stableMetaPaths), gc.ShouldEqual, 1)
// 			gc.So(len(stablePaths[0]), gc.ShouldEqual, 3)
// 			gc.So(len(stableMetaPaths[0]), gc.ShouldEqual, 3)
// 			gc.So(wavefront.phaseForValue(stablePaths[0][1]), gc.ShouldEqual, numeric.Phase(64))
// 			gc.So(wavefront.phaseForValue(stablePaths[0][2]), gc.ShouldEqual, numeric.Phase(200))
// 			gc.So(stablePaths[0][1].Mutable(), gc.ShouldBeTrue)
// 			gc.So(data.Opcode(stablePaths[0][1].Opcode()), gc.ShouldEqual, data.OpcodeNext)
// 		})
// 	})
// }

// func TestGraphServerStabilizePathsUsesPathWavefront(t *testing.T) {
// 	gc.Convey("Given a GraphServer configured with a substrate path wavefront", t, func() {
// 		ctx := context.Background()
// 		workerPool := pool.New(ctx, 1, 1, nil)
// 		defer workerPool.Close()

// 		graph := NewGraphServer(
// 			GraphWithContext(ctx),
// 			GraphWithWorkerPool(workerPool),
// 			GraphWithPathWavefront(NewPathWavefront(PathWavefrontWithAnchors(1, 10))),
// 		)
// 		defer graph.Close()

// 		currentValue := WavefrontValue(10, 206, data.OpcodeNext)
// 		anchorValue := WavefrontValue(200, 0, data.OpcodeHalt)
// 		meta := data.MustNewValue()

// 		paths := [][]data.Value{{currentValue, anchorValue}}
// 		metaPaths := [][]data.Value{{meta, meta}}

// 		gc.Convey("GraphServer should stabilize phase-bearing paths before fold", func() {
// 			stablePaths, stableMetaPaths, err := graph.stabilizePaths(paths, metaPaths)

// 			gc.So(err, gc.ShouldBeNil)
// 			gc.So(len(stablePaths), gc.ShouldEqual, 1)
// 			gc.So(len(stableMetaPaths), gc.ShouldEqual, 1)
// 			gc.So(len(stablePaths[0]), gc.ShouldEqual, 2)
// 			gc.So(graph.pathWavefront.phaseForValue(stablePaths[0][1]), gc.ShouldEqual, numeric.Phase(200))
// 		})
// 	})
// }

func BenchmarkPathWavefrontStabilize(b *testing.B) {
	paths := make([][]primitive.Value, 0, 32)
	metaPaths := make([][]primitive.Value, 0, 32)

	for range 32 {
		values, metaValues := compiledSequence([]byte("Roy is in the Kitchen"))
		paths = append(paths, values)
		metaPaths = append(metaPaths, metaValues)
	}

	wavefront := NewWavefront(WavefrontWithAnchors(4, 3))

	b.ResetTimer()

	for b.Loop() {
		_, _, _ = wavefront.Stabilize(paths, metaPaths)
	}
}
