package substrate

import (
	"context"
	"testing"

	capnp "capnproto.org/go/capnp/v3"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/system/pool"
)

/*
graphValueMatrixToPointerList builds the Graph RPC matrix payload used by Prompt.
*/
func graphValueMatrixToPointerList(values [][]primitive.Value) (capnp.PointerList, error) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		return capnp.PointerList{}, err
	}

	list, err := capnp.NewPointerList(seg, int32(len(values)))
	if err != nil {
		return capnp.PointerList{}, err
	}

	for index, row := range values {
		valueList, err := primitive.NewValue_List(seg, int32(len(row)))
		if err != nil {
			return capnp.PointerList{}, err
		}

		for valueIndex, value := range row {
			dst := valueList.At(valueIndex)
			dst.CopyFrom(value)
		}

		if err := list.Set(index, valueList.ToPtr()); err != nil {
			return capnp.PointerList{}, err
		}
	}

	return list, nil
}

/*
compiledSequence builds prompt and meta paths the same way the VM does.
*/
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

/*
wavefrontValue creates an operator-bearing Value for stabilization tests.
*/
func wavefrontValue(
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

/*
sameValueBlocks verifies that two Values carry identical raw state.
*/
func sameValueBlocks(left primitive.Value, right primitive.Value) bool {
	for index := range 8 {
		if left.Block(index) != right.Block(index) {
			return false
		}
	}

	return true
}

/*
decodeObservableSymbols extracts lexical bytes from observable Values.
*/
func decodeObservableSymbols(values []primitive.Value) []byte {
	result := make([]byte, 0, len(values))

	for _, value := range values {
		symbol, ok := primitive.InferLexicalSeed(value)
		if !ok {
			continue
		}

		result = append(result, symbol)
	}

	return result
}

/*
newGraphTestServer creates a GraphServer with the runtime prerequisites it needs.
*/
func newGraphTestServer(ctx context.Context, opts ...GraphOpt) (*GraphServer, *pool.Pool) {
	workerPool := pool.New(ctx, 1, 1, nil)
	graphOpts := []GraphOpt{
		GraphWithContext(ctx),
		GraphWithWorkerPool(workerPool),
	}
	graphOpts = append(graphOpts, opts...)

	return NewGraphServer(graphOpts...), workerPool
}

func TestGraphServerPromptPreservesPromptPrefix(t *testing.T) {
	Convey("Given a GraphServer receiving prompt and meta paths", t, func() {
		ctx := context.Background()
		graph, workerPool := newGraphTestServer(ctx)
		defer workerPool.Close()
		defer graph.Close()

		client := graph.Client("logic/substrate/prompt")
		promptValues, metaValues := compiledSequence(
			[]byte("Roy is in the kitchen. Harold is in the garden."),
		)

		Convey("Prompt should preserve the exact prompt prefix and append continuation values", func() {
			paths, err := graphValueMatrixToPointerList([][]primitive.Value{promptValues})
			So(err, ShouldBeNil)

			metaPaths, err := graphValueMatrixToPointerList([][]primitive.Value{metaValues})
			So(err, ShouldBeNil)

			future, release := client.Prompt(ctx, func(params Graph_prompt_Params) error {
				if err := params.SetPaths(paths); err != nil {
					return err
				}

				return params.SetMetaPaths(metaPaths)
			})
			defer release()

			result, err := future.Struct()
			So(err, ShouldBeNil)

			rows, err := result.Result()
			So(err, ShouldBeNil)
			So(rows.Len(), ShouldEqual, 1)

			ptr, err := rows.At(0)
			So(err, ShouldBeNil)

			values, err := primitive.ValueListToSlice(primitive.Value_List(ptr.List()))
			So(err, ShouldBeNil)
			So(len(values), ShouldBeGreaterThanOrEqualTo, len(promptValues))

			for index := range promptValues {
				So(sameValueBlocks(values[index], promptValues[index]), ShouldBeTrue)
			}
		})
	})
}

func TestRecursiveFold(t *testing.T) {
	Convey("Given a GraphServer receiving token writes", t, func() {
		ctx := context.Background()
		graph, workerPool := newGraphTestServer(ctx)
		defer workerPool.Close()
		defer graph.Close()
		client := graph.Client("logic/substrate/write")
		coder := data.NewMortonCoder()
		payload := []byte(
			"Roy is in the kitchen. Harold is in the garden. Sandra is in the library.",
		)
		batchSize, err := graph.writeFoldBatchSize()
		So(err, ShouldBeNil)
		So(batchSize, ShouldBeGreaterThan, 1)

		Convey("RecursiveFold should wait for a whole batch, then run on Write", func() {
			for index := 0; index < batchSize-1; index++ {
				writeErr := client.Write(ctx, func(params Graph_write_Params) error {
					params.SetKey(
						coder.Pack(uint32(index), payload[index%len(payload)]),
					)

					return nil
				})
				So(writeErr, ShouldBeNil)
			}
			So(client.WaitStreaming(), ShouldBeNil)

			So(len(graph.astRoots), ShouldEqual, 0)

			writeErr := client.Write(ctx, func(params Graph_write_Params) error {
				params.SetKey(
					coder.Pack(uint32(batchSize-1), payload[(batchSize-1)%len(payload)]),
				)

				return nil
			})
			So(writeErr, ShouldBeNil)
			So(client.WaitStreaming(), ShouldBeNil)
			So(len(graph.astRoots), ShouldEqual, 1)
			So(graph.astRoots[0], ShouldNotBeNil)
		})

	})
}

func TestGraphServerPromptRecordsMacroTools(t *testing.T) {
	Convey("Given a GraphServer with a shared macro index", t, func() {
		ctx := context.Background()
		index := macro.NewMacroIndexServer(
			macro.MacroIndexWithContext(ctx),
		)
		defer index.Close()

		graph, workerPool := newGraphTestServer(
			ctx,
			GraphWithMacroIndex(index),
		)
		defer workerPool.Close()
		defer graph.Close()

		client := graph.Client("logic/substrate/prompt/macro")
		promptValues, metaValues := compiledSequence([]byte("Roy is in the "))

		Convey("Prompt should derive and record the boundary tool in MacroIndex", func() {
			paths, err := graphValueMatrixToPointerList([][]primitive.Value{promptValues})
			So(err, ShouldBeNil)

			metaPaths, err := graphValueMatrixToPointerList([][]primitive.Value{metaValues})
			So(err, ShouldBeNil)

			future, release := client.Prompt(ctx, func(params Graph_prompt_Params) error {
				if err := params.SetPaths(paths); err != nil {
					return err
				}

				return params.SetMetaPaths(metaPaths)
			})
			defer release()

			_, err = future.Struct()
			So(err, ShouldBeNil)

			So(len(promptValues), ShouldBeGreaterThan, 1)

			start := primitiveFromDataForGraphTest(promptValues[0])
			end := primitiveFromDataForGraphTest(promptValues[len(promptValues)-1])
			key := macro.AffineKeyFromValues(start, end)

			opcode, found := index.FindOpcode(key)
			So(found, ShouldBeTrue)
			So(opcode, ShouldNotBeNil)
			So(opcode.UseCount, ShouldBeGreaterThanOrEqualTo, uint64(1))
		})
	})
}

func TestGraphServerPromptTracksCandidateOutcomes(t *testing.T) {
	Convey("Given a GraphServer repeatedly prompted with one boundary pair", t, func() {
		ctx := context.Background()
		index := macro.NewMacroIndexServer(
			macro.MacroIndexWithContext(ctx),
		)
		defer index.Close()

		graph, workerPool := newGraphTestServer(
			ctx,
			GraphWithMacroIndex(index),
		)
		defer workerPool.Close()
		defer graph.Close()

		client := graph.Client("logic/substrate/prompt/candidate")
		promptValues, metaValues := compiledSequence([]byte("Roy is in the "))
		start := primitiveFromDataForGraphTest(promptValues[0])
		end := primitiveFromDataForGraphTest(promptValues[len(promptValues)-1])
		key := macro.AffineKeyFromValues(start, end)

		Convey("Prompt should accumulate candidate success/failure counts per run", func() {
			paths, err := graphValueMatrixToPointerList([][]primitive.Value{promptValues})
			So(err, ShouldBeNil)

			metaPaths, err := graphValueMatrixToPointerList([][]primitive.Value{metaValues})
			So(err, ShouldBeNil)

			const promptRuns = 3
			for range promptRuns {
				future, release := client.Prompt(ctx, func(params Graph_prompt_Params) error {
					if err := params.SetPaths(paths); err != nil {
						return err
					}

					return params.SetMetaPaths(metaPaths)
				})

				_, runErr := future.Struct()
				release()
				So(runErr, ShouldBeNil)
			}

			candidate, found := index.FindCandidate(key)
			So(found, ShouldBeTrue)
			So(candidate, ShouldNotBeNil)
			So(candidate.PreResidue, ShouldBeGreaterThanOrEqualTo, 0)
			So(candidate.PostResidue, ShouldBeGreaterThanOrEqualTo, 0)
			So(candidate.SuccessCount+candidate.FailureCount, ShouldEqual, uint64(promptRuns))
		})
	})
}

func TestResultsFromStoredData(t *testing.T) {
	Convey("Given stored graph data and lexical prompt symbols", t, func() {
		ctx := context.Background()
		graph, workerPool := newGraphTestServer(ctx)
		defer workerPool.Close()
		defer graph.Close()

		coder := data.NewMortonCoder()
		payload := []byte(
			"Roy is in the kitchen. Harold is in the garden. " +
				"The quick brown fox jumps over the lazy dog. " +
				"The quick red fox jumps over the sleeping dog.",
		)
		graph.data = make([]uint64, len(payload))

		for index, symbol := range payload {
			graph.data[index] = coder.Pack(uint32(index), symbol)
		}

		Convey("resultsFromStoredData should trim the matched continuation at sentence boundary", func() {
			promptValues, _ := compiledSequence([]byte("is in the "))
			results := graph.resultsFromStoredData(
				[][]primitive.Value{toPrimitivePath(promptValues)},
				[][]byte{[]byte("is in the ")},
			)

			So(len(results), ShouldEqual, 1)
			So(string(decodePrimitiveSymbols(results[0])), ShouldEqual, "is in the kitchen.")
		})

		Convey("resultsFromStoredData should fall back to prompt path when no exact match exists", func() {
			promptValues, _ := compiledSequence([]byte("non-existent prompt"))
			primitivePath := toPrimitivePath(promptValues)
			results := graph.resultsFromStoredData(
				[][]primitive.Value{primitivePath},
				[][]byte{[]byte("non-existent prompt")},
			)

			So(len(results), ShouldEqual, 1)
			So(len(results[0]), ShouldEqual, len(primitivePath))

			for index := range primitivePath {
				So(samePrimitiveBlocks(results[0][index], primitivePath[index]), ShouldBeTrue)
			}
		})

		Convey("resultsFromStoredData should choose the strongest exact match when multiple windows fit", func() {
			promptValues, _ := compiledSequence([]byte("The quick "))
			results := graph.resultsFromStoredData(
				[][]primitive.Value{toPrimitivePath(promptValues)},
				[][]byte{[]byte("The quick ")},
			)

			So(len(results), ShouldEqual, 1)
			So(string(decodePrimitiveSymbols(results[0])), ShouldEqual, "The quick red fox jumps over the sleeping dog.")
		})
	})
}

func TestDecodePromptSymbols(t *testing.T) {
	Convey("Given observable prompt values", t, func() {
		ctx := context.Background()
		graph, workerPool := newGraphTestServer(ctx)
		defer workerPool.Close()
		defer graph.Close()

		values, _ := compiledSequence([]byte("Roy"))
		primitivePath := make([]primitive.Value, len(values))
		for index, value := range values {
			primitivePath[index] = primitiveFromDataForGraphTest(value)
		}

		Convey("decodePromptSymbols should recover lexical bytes", func() {
			symbols := graph.decodePromptSymbols(primitivePath)
			So(string(symbols), ShouldEqual, "Roy")
		})

		valuesWithSpaces, _ := compiledSequence([]byte("Roy is in the "))
		primitivePathWithSpaces := make([]primitive.Value, len(valuesWithSpaces))
		for index, value := range valuesWithSpaces {
			primitivePathWithSpaces[index] = primitiveFromDataForGraphTest(value)
		}

		Convey("decodePromptSymbols should keep spaces in lexical prompts", func() {
			symbols := graph.decodePromptSymbols(primitivePathWithSpaces)
			So(string(symbols), ShouldEqual, "Roy is in the ")
		})
	})
}

/*
primitiveFromDataForGraphTest clones data.Value into primitive.Value for key checks.
*/
func primitiveFromDataForGraphTest(value primitive.Value) primitive.Value {
	out, err := primitive.New()
	if err != nil {
		panic(err)
	}

	out.SetC0(value.C0())
	out.SetC1(value.C1())
	out.SetC2(value.C2())
	out.SetC3(value.C3())
	out.SetC4(value.C4())
	out.SetC5(value.C5())
	out.SetC6(value.C6())
	out.SetC7(value.C7())

	return out
}

/*
toPrimitivePath maps one observable data.Value path into primitive.Value.
*/
func toPrimitivePath(values []primitive.Value) []primitive.Value {
	out := make([]primitive.Value, len(values))

	for index, value := range values {
		out[index] = primitiveFromDataForGraphTest(value)
	}

	return out
}

/*
decodePrimitiveSymbols extracts lexical bytes from primitive-value paths.
*/
func decodePrimitiveSymbols(values []primitive.Value) []byte {
	out := make([]byte, 0, len(values))

	for _, value := range values {
		symbol, ok := primitive.InferLexicalSeed(primitive.Value(value))
		if !ok {
			continue
		}

		out = append(out, symbol)
	}

	return out
}

/*
samePrimitiveBlocks verifies that two primitive values carry identical raw state.
*/
func samePrimitiveBlocks(left primitive.Value, right primitive.Value) bool {
	for index := range 8 {
		if left.Block(index) != right.Block(index) {
			return false
		}
	}

	return true
}

/*
BenchmarkResultsFromStoredData measures continuation match and trimming throughput.
*/
func BenchmarkResultsFromStoredData(b *testing.B) {
	ctx := context.Background()
	graph, workerPool := newGraphTestServer(ctx)
	defer workerPool.Close()
	defer graph.Close()

	coder := data.NewMortonCoder()
	payload := []byte("Roy is in the kitchen. Harold is in the garden.")
	graph.data = make([]uint64, len(payload))

	for index, symbol := range payload {
		graph.data[index] = coder.Pack(uint32(index), symbol)
	}

	promptValues, _ := compiledSequence([]byte("is in the "))
	promptPath := toPrimitivePath(promptValues)
	metaPromptSymbols := [][]byte{[]byte("is in the ")}

	b.ResetTimer()

	for b.Loop() {
		results := graph.resultsFromStoredData(
			[][]primitive.Value{promptPath},
			metaPromptSymbols,
		)

		if len(results) != 1 {
			b.Fatalf("expected one result path, got %d", len(results))
		}

		if string(decodePrimitiveSymbols(results[0])) != "is in the kitchen." {
			b.Fatalf("unexpected continuation: %q", string(decodePrimitiveSymbols(results[0])))
		}
	}
}

// func TestGraphServerStabilizePaths(t *testing.T) {
// 	Convey("Given a GraphServer configured with an anchored path wavefront", t, func() {
// 		ctx := context.Background()
// 		graph, workerPool := newGraphTestServer(
// 			ctx,
// 			GraphWithPathWavefront(path.NewWavefront(path.WavefrontWithAnchors(1, 10))),
// 		)
// 		defer workerPool.Close()
// 		defer graph.Close()

// 		currentValue := wavefrontValue(10, 206, data.OpcodeNext)
// 		anchorValue := wavefrontValue(200, 0, data.OpcodeHalt)
// 		meta := data.MustNewValue()

// 		paths := [][]data.Value{{currentValue, anchorValue}}
// 		metaPaths := [][]data.Value{{meta, meta}}

// 		Convey("stabilizePaths should snap the drifted transition before fold", func() {
// 			stablePaths, stableMetaPaths, err := graph.stabilizePaths(paths, metaPaths)

// 			So(err, ShouldBeNil)
// 			So(len(stablePaths), ShouldEqual, 1)
// 			So(len(stableMetaPaths), ShouldEqual, 1)
// 			So(len(stablePaths[0]), ShouldEqual, 2)
// 			So(graph.pathWavefront, ShouldNotBeNil)
// 			So(graph.pathWavefront.CanStabilize(stablePaths), ShouldBeTrue)
// 		})
// 	})
// }

// func TestGraphServerRetainsHighResolutionProgramContext(t *testing.T) {
// 	Convey("Given two distinct high-resolution Value contexts", t, func() {
// 		ctx := context.Background()
// 		graph, workerPool := newGraphTestServer(ctx)
// 		defer workerPool.Close()
// 		defer graph.Close()

// 		leftABase := data.BaseValue('A')
// 		leftAValue := leftABase.RollLeft(11)
// 		leftA := data.SeedObservable('A', leftAValue)

// 		leftBBase := data.BaseValue('A')
// 		leftBValue := leftBBase.RollLeft(37)
// 		leftB := data.SeedObservable('A', leftBValue)
// 		meta := data.MustNewValue()

// 		Convey("RecursiveFold should not collapse them onto one program selection just because a scalar phase component could collide", func() {
// 			results, err := graph.foldPaths(
// 				[][]data.Value{{leftA}, {leftB}},
// 				[][]data.Value{{meta}, {meta}},
// 			)

// 			So(err, ShouldBeNil)
// 			So(len(results), ShouldEqual, 2)
// 			So(len(results[0]), ShouldBeGreaterThan, 1)
// 			So(len(results[1]), ShouldBeGreaterThan, 1)
// 			So(sameValueBlocks(results[0][1], results[1][1]), ShouldBeFalse)
// 		})
// 	})
// }

// func TestGraphServerResultsFromStoredData(t *testing.T) {
// 	Convey("Given stored dataset keys and a prompt prefix", t, func() {
// 		ctx := context.Background()
// 		graph, workerPool := newGraphTestServer(ctx)
// 		defer workerPool.Close()
// 		defer graph.Close()

// 		coder := data.NewMortonCoder()
// 		payload := []byte("Roy is in the Kitchen")
// 		graph.data = make([]uint64, len(payload))

// 		for index, symbol := range payload {
// 			graph.data[index] = coder.Pack(uint32(index), symbol)
// 		}

// 		promptValues, _ := compiledSequence([]byte("Roy is in the "))

// 		Convey("resultsFromStoredData should return the exact continuation bytes", func() {
// 			results, matched, err := graph.resultsFromStoredData([][]data.Value{promptValues})

// 			So(err, ShouldBeNil)
// 			So(matched, ShouldBeTrue)
// 			So(len(results), ShouldEqual, 1)
// 			So(len(results[0]), ShouldBeGreaterThan, len(promptValues))
// 			So(string(decodeObservableSymbols(results[0][len(promptValues):])), ShouldEqual, "Kitchen")
// 		})
// 	})
// }
