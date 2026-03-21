package synthesis

import (
	"context"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/store/data"
	dmtserver "github.com/theapemachine/six/pkg/store/dmt/server"
	"github.com/theapemachine/six/pkg/system/cluster"
)

/*
valueWithBits builds one primitive value with the specified core bits active.
*/
func valueWithBits(bits ...int) primitive.Value {
	value, err := primitive.New()
	if err != nil {
		panic(err)
	}

	for _, bit := range bits {
		value.Set(bit)
	}

	return value
}

/*
combineValues OR-composes values into one sparse fact representation.
*/
func combineValues(values ...primitive.Value) primitive.Value {
	combined := primitive.NeutralValue()

	for _, value := range values {
		next, err := combined.OR(value)
		if err != nil {
			panic(err)
		}

		combined = next
	}

	return combined
}

/*
hasBit reports whether one core bit index is active.
*/
func hasBit(value primitive.Value, index int) bool {
	block := value.Block(index / 64)
	mask := uint64(1) << uint(index%64)
	return block&mask != 0
}

/*
primitiveFromDataTest clones data.Value into primitive.Value for key assertions.
*/
func primitiveFromDataTest(value primitive.Value) primitive.Value {
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
newHASTestRouter registers the capabilities HAS resolves during tests.
*/
func newHASTestRouter(
	ctx context.Context,
	includeMacro bool,
	includeForest bool,
) (*cluster.Router, *macro.MacroIndexServer, *dmtserver.ForestServer) {
	router := cluster.NewRouter(cluster.RouterWithContext(ctx))

	var index *macro.MacroIndexServer
	if includeMacro {
		index = macro.NewMacroIndexServer(
			macro.MacroIndexWithContext(ctx),
		)
		router.Register(cluster.MACROINDEX, index)
	}

	var forest *dmtserver.ForestServer
	if includeForest {
		forest = dmtserver.NewForestServer(
			dmtserver.WithContext(ctx),
		)
		router.Register(cluster.FOREST, forest)
	}

	return router, index, forest
}

/*
TestHASDerive verifies ingestion-side tool forging from known boundary pairs.
*/
func TestHASDerive(t *testing.T) {
	gc.Convey("Given a HAS server with a shared macro index", t, func() {
		router, index, _ := newHASTestRouter(context.Background(), true, false)

		server := NewHASServer(
			HASWithContext(context.Background()),
			HASWithRouter(router),
		)
		defer index.Close()
		defer router.Close()
		defer server.Close()

		/*
			deriveTestCase captures one Derive expectation for table-driven coverage.
		*/
		type deriveTestCase struct {
			name              string
			start             primitive.Value
			end               primitive.Value
			repeats           int
			expectErrorSubstr string
			expectUseCount    uint64
		}

		testCases := []deriveTestCase{
			{
				name:           "Derive should create and reuse the same affine key",
				start:          primitiveFromDataTest(primitive.BaseValue('A')),
				end:            primitiveFromDataTest(primitive.BaseValue('B')),
				repeats:        2,
				expectUseCount: 2,
			},
			{
				name:              "Derive should reject empty start",
				start:             primitive.Value{},
				end:               primitiveFromDataTest(primitive.BaseValue('B')),
				repeats:           1,
				expectErrorSubstr: string(HASErrorTypeStartAndEndRequired),
			},
			{
				name:              "Derive should reject empty end",
				start:             primitiveFromDataTest(primitive.BaseValue('A')),
				end:               primitive.Value{},
				repeats:           1,
				expectErrorSubstr: string(HASErrorTypeStartAndEndRequired),
			},
		}

		for _, testCase := range testCases {
			testCase := testCase

			gc.Convey(testCase.name, func() {
				var (
					key    macro.AffineKey
					opcode *macro.MacroOpcode
					err    error
				)

				for range testCase.repeats {
					key, opcode, err = server.Derive(testCase.start, testCase.end)
				}

				if testCase.expectErrorSubstr != "" {
					gc.So(err, gc.ShouldNotBeNil)
					gc.So(err.Error(), gc.ShouldContainSubstring, testCase.expectErrorSubstr)
					gc.So(opcode, gc.ShouldBeNil)
					return
				}

				gc.So(err, gc.ShouldBeNil)
				gc.So(opcode, gc.ShouldNotBeNil)
				gc.So(opcode.UseCount, gc.ShouldEqual, testCase.expectUseCount)

				stored, found := index.FindOpcode(key)
				gc.So(found, gc.ShouldBeTrue)
				gc.So(stored, gc.ShouldNotBeNil)
				gc.So(stored.UseCount, gc.ShouldEqual, testCase.expectUseCount)
			})
		}
	})
}

/*
TestHASWriteDone verifies RPC ingestion path derives/stores one macro tool.
*/
func TestHASWriteDone(t *testing.T) {
	gc.Convey("Given a HAS server receiving one start/end pair over RPC", t, func() {
		router, index, _ := newHASTestRouter(context.Background(), true, false)

		server := NewHASServer(
			HASWithContext(context.Background()),
			HASWithRouter(router),
		)
		defer index.Close()
		defer router.Close()
		defer server.Close()

		client := HAS(server.Client("logic/synthesis/has_test"))
		start := primitive.BaseValue('S')
		end := primitive.BaseValue('E')

		writeErr := client.Write(context.Background(), func(params HAS_write_Params) error {
			if err := params.SetStart(start); err != nil {
				return err
			}

			return params.SetEnd(end)
		})
		gc.So(writeErr, gc.ShouldBeNil)

		doneFuture, release := client.Done(context.Background(), nil)
		defer release()

		doneResult, doneErr := doneFuture.Struct()
		gc.So(doneErr, gc.ShouldBeNil)

		keyText, keyTextErr := doneResult.KeyText()
		gc.So(keyTextErr, gc.ShouldBeNil)
		gc.So(doneResult.UseCount(), gc.ShouldEqual, uint64(1))
		gc.So(doneResult.Hardened(), gc.ShouldBeFalse)

		key := macro.AffineKeyFromValues(
			primitiveFromDataTest(start),
			primitiveFromDataTest(end),
		)

		gc.So(keyText, gc.ShouldEqual, key.String())

		opcode, found := index.FindOpcode(key)
		gc.So(found, gc.ShouldBeTrue)
		gc.So(opcode, gc.ShouldNotBeNil)
		gc.So(opcode.UseCount, gc.ShouldEqual, uint64(1))
	})
}

/*
TestHASLoad verifies the load signal is derived from staged boundary state.
*/
func TestHASLoad(t *testing.T) {
	gc.Convey("Given a HAS server with staged boundaries", t, func() {
		server := NewHASServer(
			HASWithContext(context.Background()),
		)
		defer server.Close()

		gc.So(server.Load(), gc.ShouldEqual, int64(0))

		server.copyDataIntoPrimitive(&server.start, primitive.BaseValue('S'))
		server.copyDataIntoPrimitive(&server.end, primitive.BaseValue('E'))

		gc.Convey("Load should reflect the staged boundary pressure", func() {
			gc.So(server.Load(), gc.ShouldBeGreaterThan, int64(0))
		})
	})
}

/*
BenchmarkHASDerive measures affine tool-forging throughput from known boundaries.
*/
func BenchmarkHASDerive(b *testing.B) {
	router, index, _ := newHASTestRouter(context.Background(), true, false)

	server := NewHASServer(
		HASWithContext(context.Background()),
		HASWithRouter(router),
	)
	defer index.Close()
	defer router.Close()
	defer server.Close()

	start := primitiveFromDataTest(primitive.BaseValue('A'))
	end := primitiveFromDataTest(primitive.BaseValue('B'))

	b.ResetTimer()

	for b.Loop() {
		_, opcode, err := server.Derive(start, end)
		if err != nil {
			b.Fatalf("derive failed: %v", err)
		}

		if opcode == nil {
			b.Fatalf("derive returned nil opcode")
		}
	}
}

/*
BenchmarkHASWriteDone measures RPC ingestion throughput for boundary pairs.
*/
func BenchmarkHASWriteDone(b *testing.B) {
	router, index, _ := newHASTestRouter(context.Background(), true, false)

	server := NewHASServer(
		HASWithContext(context.Background()),
		HASWithRouter(router),
	)
	defer index.Close()
	defer router.Close()
	defer server.Close()

	client := HAS(server.Client("logic/synthesis/has_benchmark"))
	start := primitive.BaseValue('S')
	end := primitive.BaseValue('E')

	b.ResetTimer()

	for b.Loop() {
		writeErr := client.Write(context.Background(), func(params HAS_write_Params) error {
			if err := params.SetStart(start); err != nil {
				return err
			}

			return params.SetEnd(end)
		})
		if writeErr != nil {
			b.Fatalf("write failed: %v", writeErr)
		}

		doneFuture, release := client.Done(context.Background(), nil)
		_, doneErr := doneFuture.Struct()
		release()

		if doneErr != nil {
			b.Fatalf("done failed: %v", doneErr)
		}
	}
}

/*
TestHASDoneCompletes verifies Done finishes the Derive path for a seeded forest.
*/
func TestHASDoneCompletes(t *testing.T) {
	gc.Convey("Given a HAS server with distinct branch symbols in the forest", t, func() {
		router, index, forest := newHASTestRouter(context.Background(), true, true)
		defer index.Close()
		defer forest.Close()
		defer router.Close()

		server := NewHASServer(
			HASWithContext(context.Background()),
			HASWithRouter(router),
		)
		defer server.Close()
		promptSymbol := byte('E')
		nextSymbol := byte('X')
		forestClient := dmtserver.Server_ServerToClient(forest)
		defer forestClient.Release()

		writeKey := func(position uint32, symbol byte) {
			writeErr := forestClient.Write(context.Background(), func(params dmtserver.Server_write_Params) error {
				params.SetKey(data.NewMortonCoder().Pack(position, symbol))
				return nil
			})
			gc.So(writeErr, gc.ShouldBeNil)
		}

		writeKey(1, promptSymbol)
		writeKey(2, nextSymbol)

		client := HAS(server.Client("logic/synthesis/has_test/program-error-propagation"))
		start := primitive.BaseValue('S')
		end := primitive.BaseValue(promptSymbol)

		writeErr := client.Write(context.Background(), func(params HAS_write_Params) error {
			if err := params.SetStart(start); err != nil {
				return err
			}

			return params.SetEnd(end)
		})
		gc.So(writeErr, gc.ShouldBeNil)

		doneFuture, release := client.Done(context.Background(), nil)
		defer release()

		results, doneErr := doneFuture.Struct()
		gc.So(doneErr, gc.ShouldBeNil)

		keyText, ktErr := results.KeyText()
		gc.So(ktErr, gc.ShouldBeNil)
		gc.So(keyText, gc.ShouldNotBeEmpty)
	})
}

/*
TestHASCollectPromptBranchesViaRouter verifies HAS resolves forest branches through the router.
*/
func TestHASCollectPromptBranchesViaRouter(t *testing.T) {
	gc.Convey("Given a HAS server with forest capability on the router", t, func() {
		router, _, forest := newHASTestRouter(context.Background(), false, true)
		defer forest.Close()
		defer router.Close()

		server := NewHASServer(
			HASWithContext(context.Background()),
			HASWithRouter(router),
		)
		defer server.Close()

		forestClient := dmtserver.Server_ServerToClient(forest)
		defer forestClient.Release()

		writeKey := func(position uint32, symbol byte) {
			writeErr := forestClient.Write(context.Background(), func(params dmtserver.Server_write_Params) error {
				params.SetKey(data.NewMortonCoder().Pack(position, symbol))
				return nil
			})
			gc.So(writeErr, gc.ShouldBeNil)
		}

		writeKey(1, 'E')
		writeKey(2, 'X')
		writeKey(2, 'Y')

		branches, err := server.collectPromptBranches(primitive.BaseValue('E'))
		gc.So(err, gc.ShouldBeNil)
		gc.So(len(branches), gc.ShouldEqual, 2)

		firstSymbol, ok := primitive.InferLexicalSeed(branches[0])
		gc.So(ok, gc.ShouldBeTrue)
		gc.So(firstSymbol, gc.ShouldEqual, byte('X'))

		secondSymbol, ok := primitive.InferLexicalSeed(branches[1])
		gc.So(ok, gc.ShouldBeTrue)
		gc.So(secondSymbol, gc.ShouldEqual, byte('Y'))
	})
}

/*
TestHASRouterFailures verifies HAS fails cleanly when routed capabilities are missing.
*/
func TestHASRouterFailures(t *testing.T) {
	gc.Convey("Given a HAS server without a router", t, func() {
		server := NewHASServer(
			HASWithContext(context.Background()),
		)
		defer server.Close()

		_, _, err := server.Derive(primitive.BaseValue('A'), primitive.BaseValue('B'))
		gc.So(err, gc.ShouldNotBeNil)
		gc.So(err.Error(), gc.ShouldContainSubstring, string(HASErrorTypeRouterRequired))
	})

	gc.Convey("Given a HAS server with a router but no macro index capability", t, func() {
		router := cluster.NewRouter(cluster.RouterWithContext(context.Background()))
		defer router.Close()

		server := NewHASServer(
			HASWithContext(context.Background()),
			HASWithRouter(router),
		)
		defer server.Close()

		_, _, err := server.Derive(primitive.BaseValue('A'), primitive.BaseValue('B'))
		gc.So(err, gc.ShouldNotBeNil)
		gc.So(err.Error(), gc.ShouldContainSubstring, "router: no service registered for")
	})

	gc.Convey("Given a HAS server with a router but no forest capability", t, func() {
		router := cluster.NewRouter(cluster.RouterWithContext(context.Background()))
		defer router.Close()

		server := NewHASServer(
			HASWithContext(context.Background()),
			HASWithRouter(router),
		)
		defer server.Close()

		_, err := server.collectPromptBranches(primitive.BaseValue('E'))
		gc.So(err, gc.ShouldNotBeNil)
		gc.So(err.Error(), gc.ShouldContainSubstring, "router: no service registered for")
	})
}
