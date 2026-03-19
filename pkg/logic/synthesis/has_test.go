package synthesis

import (
	"context"
	"encoding/binary"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/lang"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/store/dmt"
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
TestHASDerive verifies ingestion-side tool forging from known boundary pairs.
*/
func TestHASDerive(t *testing.T) {
	gc.Convey("Given a HAS server with a shared macro index", t, func() {
		index := macro.NewMacroIndexServer(
			macro.MacroIndexWithContext(context.Background()),
		)

		server := NewHASServer(
			HASWithContext(context.Background()),
			HASWithMacroIndex(index),
		)
		defer index.Close()
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
		index := macro.NewMacroIndexServer(
			macro.MacroIndexWithContext(context.Background()),
		)

		server := NewHASServer(
			HASWithContext(context.Background()),
			HASWithMacroIndex(index),
		)
		defer index.Close()
		defer server.Close()

		client := server.Client("logic/synthesis/has_test")
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
TestHASAsk verifies reagent-style inference via table-driven scenarios.
*/
func TestHASAsk(t *testing.T) {
	gc.Convey("Given HAS ask scenarios", t, func() {
		server := NewHASServer(
			HASWithContext(context.Background()),
		)
		defer server.Close()

		/*
			askTestCase captures one inference case for table-driven execution.
		*/
		type askTestCase struct {
			name                string
			known               []primitive.Value
			vat                 []primitive.Value
			expectErrorContains string
			expectWinnerIndex   int
			expectResidueBits   []int
			expectResidueCount  int
		}

		entity := valueWithBits(1, 2)
		relation := valueWithBits(10, 11)
		answer := valueWithBits(50, 51)

		testCases := []askTestCase{
			{
				name:  "Ask should recover the clean residue from the best matching fact",
				known: []primitive.Value{entity, relation},
				vat: []primitive.Value{
					combineValues(valueWithBits(3, 4), relation, valueWithBits(60)),
					combineValues(valueWithBits(100), valueWithBits(101)),
					combineValues(entity, relation, answer),
				},
				expectWinnerIndex:  2,
				expectResidueBits:  []int{50, 51},
				expectResidueCount: 2,
			},
			{
				name:                "Ask should reject empty known values",
				known:               nil,
				vat:                 []primitive.Value{valueWithBits(1)},
				expectErrorContains: string(HASErrorTypeKnownValuesRequired),
			},
			{
				name:                "Ask should reject empty vat",
				known:               []primitive.Value{valueWithBits(1)},
				vat:                 nil,
				expectErrorContains: string(HASErrorTypeVatEmpty),
			},
		}

		for _, testCase := range testCases {
			testCase := testCase

			gc.Convey(testCase.name, func() {
				outcome, err := server.Ask(testCase.known, testCase.vat)

				if testCase.expectErrorContains != "" {
					gc.So(outcome, gc.ShouldBeNil)
					gc.So(err, gc.ShouldNotBeNil)
					gc.So(err.Error(), gc.ShouldContainSubstring, testCase.expectErrorContains)
					return
				}

				gc.So(err, gc.ShouldBeNil)
				gc.So(outcome, gc.ShouldNotBeNil)
				gc.So(outcome.WinnerIndex, gc.ShouldEqual, testCase.expectWinnerIndex)
				gc.So(outcome.Residue.CoreActiveCount(), gc.ShouldEqual, testCase.expectResidueCount)

				for _, bit := range testCase.expectResidueBits {
					gc.So(hasBit(outcome.Residue, bit), gc.ShouldBeTrue)
				}
			})
		}
	})
}

/*
BenchmarkHASDerive measures affine tool-forging throughput from known boundaries.
*/
func BenchmarkHASDerive(b *testing.B) {
	index := macro.NewMacroIndexServer(
		macro.MacroIndexWithContext(context.Background()),
	)

	server := NewHASServer(
		HASWithContext(context.Background()),
		HASWithMacroIndex(index),
	)
	defer index.Close()
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
BenchmarkHASAsk measures reagent-style inference against a small fact vat.
*/
func BenchmarkHASAsk(b *testing.B) {
	server := NewHASServer(
		HASWithContext(context.Background()),
	)
	defer server.Close()

	entity := valueWithBits(1, 2)
	relation := valueWithBits(10, 11)
	answer := valueWithBits(50, 51)

	known := []primitive.Value{entity, relation}
	vat := []primitive.Value{
		combineValues(valueWithBits(3, 4), relation, valueWithBits(60)),
		combineValues(valueWithBits(100), valueWithBits(101)),
		combineValues(entity, relation, answer),
	}

	b.ResetTimer()

	for b.Loop() {
		outcome, err := server.Ask(known, vat)
		if err != nil {
			b.Fatalf("ask failed: %v", err)
		}

		if outcome == nil || outcome.WinnerIndex != 2 {
			b.Fatalf("ask returned unexpected winner: %+v", outcome)
		}
	}
}

/*
BenchmarkHASWriteDone measures RPC ingestion throughput for boundary pairs.
*/
func BenchmarkHASWriteDone(b *testing.B) {
	index := macro.NewMacroIndexServer(
		macro.MacroIndexWithContext(context.Background()),
	)

	server := NewHASServer(
		HASWithContext(context.Background()),
		HASWithMacroIndex(index),
	)
	defer index.Close()
	defer server.Close()

	client := server.Client("logic/synthesis/has_benchmark")
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
TestHASDonePropagatesProgramExecutionErrors verifies Done returns execution failures instead of swallowing them.
*/
func TestHASDonePropagatesProgramExecutionErrors(t *testing.T) {
	gc.Convey("Given a HAS server with branch candidates that cannot phase-lock", t, func() {
		index := macro.NewMacroIndexServer(
			macro.MacroIndexWithContext(context.Background()),
		)
		defer index.Close()

		forest, forestErr := dmt.NewForest(dmt.ForestConfig{})
		gc.So(forestErr, gc.ShouldBeNil)
		defer forest.Close()

		server := NewHASServer(
			HASWithContext(context.Background()),
			HASWithMacroIndex(index),
			HASWithForest(forest),
		)
		defer server.Close()

		program := lang.NewProgramServer(
			lang.ProgramServerWithContext(context.Background()),
			lang.ProgramServerWithMacroIndex(index),
			lang.ProgramServerWithMaxSteps(1),
		)
		defer program.Close()
		server.program = program

		coder := data.NewMortonCoder()
		promptSymbol := byte('E')
		nextSymbol := byte('X')

		promptKey := make([]byte, 8)
		nextKey := make([]byte, 8)

		binary.BigEndian.PutUint64(promptKey, coder.Pack(1, promptSymbol))
		binary.BigEndian.PutUint64(nextKey, coder.Pack(2, nextSymbol))

		forest.Insert(promptKey, []byte{1})
		forest.Insert(nextKey, []byte{1})

		client := server.Client("logic/synthesis/has_test/program-error-propagation")
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

		_, doneErr := doneFuture.Struct()
		gc.So(doneErr, gc.ShouldNotBeNil)
		gc.So(doneErr.Error(), gc.ShouldContainSubstring, string(lang.ProgramErrorTypeProgramStalled))
	})
}
