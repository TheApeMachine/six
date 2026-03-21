package bvp

import (
	"context"
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/logic/synthesis/macro"
	"github.com/theapemachine/six/pkg/system/cluster"
	"github.com/theapemachine/six/pkg/system/process/tokenizer"
	"github.com/theapemachine/six/pkg/system/vm/input"
)

func TestCantileverPromptValues(t *testing.T) {
	Convey("Given a cantilever server with tokenizer and prompter capabilities", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		router := cluster.NewRouter(cluster.RouterWithContext(ctx))
		tok := tokenizer.NewUniversalServer(
			tokenizer.UniversalWithContext(ctx),
		)
		defer tok.Close()

		prompter := input.NewPrompterServer(
			input.PrompterWithContext(ctx),
		)
		defer prompter.Close()

		router.Register(cluster.TOKENIZER, tok)
		router.Register(cluster.PROMPTER, prompter)

		server := NewCantileverServer(
			CantileverWithContext(ctx),
			CantileverWithRouter(router),
		)

		client := tokenizer.Universal(tok.Client("test"))
		for _, symbol := range []byte("Roy is in the Kitchen") {
			err := client.Write(ctx, func(params tokenizer.Universal_write_Params) error {
				params.SetData(symbol)
				return nil
			})
			So(err, ShouldBeNil)
		}

		So(client.WaitStreaming(), ShouldBeNil)

		keys, err := server.tokenizerKeys(ctx, client)
		So(err, ShouldBeNil)

		server.Store([][]primitive.Value{
			primitive.CompileObservableSequenceValues(keys),
		})

		Convey("It should recover the exact continuation suffix", func() {
			promptValues, err := server.promptValues(ctx, []byte("Roy is in the "))

			So(err, ShouldBeNil)
			So(string(decodePromptValues(promptValues)), ShouldEqual, "Roy is in the ")
			So(string(decodePromptValues(server.exactContinuation(promptValues))), ShouldEqual, "Kitchen")
		})
	})
}

func TestCantilever(t *testing.T) {
	ctx := context.Background()

	cases := map[string]struct {
		StartByte   byte
		GoalByte    byte
		Repeats     int
		ExpectError bool
	}{
		"normal_forward_span": {
			StartByte:   50,
			GoalByte:    210,
			Repeats:     1,
			ExpectError: false,
		},
		"normal_backward_span": {
			StartByte:   210,
			GoalByte:    50,
			Repeats:     1,
			ExpectError: false,
		},
		"edge_of_field_to_edge": {
			StartByte:   1,
			GoalByte:    254,
			Repeats:     1,
			ExpectError: false,
		},
		"span_requires_hardening": {
			StartByte:   15,
			GoalByte:    88,
			Repeats:     10,
			ExpectError: false,
		},
	}

	for name, tc := range cases {
		Convey(fmt.Sprintf("Given case: %s", name), t, func() {
			macroIndex := macro.NewMacroIndexServer(
				macro.MacroIndexWithContext(ctx),
			)

			startValue := primitive.BaseValue(tc.StartByte)
			goalValue := primitive.BaseValue(tc.GoalByte)

			cl := NewCantileverServer(
				CantileverWithContext(ctx),
				WithMacroIndex(macroIndex),
			)

			var lastOp *macro.MacroOpcode
			var lastKey macro.AffineKey
			var lastErr error

			for range tc.Repeats {
				lastKey, lastOp, lastErr = cl.BridgeValues(startValue, goalValue)
			}

			if tc.ExpectError {
				Convey(fmt.Sprintf("%s: bridging should return an error", name), func() {
					So(lastErr, ShouldNotBeNil)
					So(lastOp, ShouldBeNil)
				})
			} else {
				Convey(fmt.Sprintf("%s: bridging should synthesize the correct affine key", name), func() {
					So(lastErr, ShouldBeNil)

					expectedKey := macro.AffineKeyFromValues(
						primitive.Value(startValue),
						primitive.Value(goalValue),
					)
					t.Logf("lastOp.Key: %v, Expected key: %v", lastOp.Key, expectedKey)
					So(lastKey, ShouldResemble, expectedKey)
					So(lastOp, ShouldNotBeNil)
					So(lastOp.Key, ShouldResemble, expectedKey)
				})

				Convey(fmt.Sprintf("%s: the generated opcode should track usage accurately", name), func() {
					So(lastOp.UseCount, ShouldEqual, tc.Repeats)

					if tc.Repeats > 5 {
						So(lastOp.Hardened, ShouldBeTrue)
					} else {
						So(lastOp.Hardened, ShouldBeFalse)
					}
				})
			}
		})
	}

	Convey("Given BridgeValues with empty values should error", t, func() {
		macroIndex := macro.NewMacroIndexServer(
			macro.MacroIndexWithContext(ctx),
		)

		cl := NewCantileverServer(
			CantileverWithContext(ctx),
			WithMacroIndex(macroIndex),
		)

		emptyValue, err := primitive.New()
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}
		realValue := primitive.BaseValue(42)

		_, _, err = cl.BridgeValues(emptyValue, realValue)
		So(err, ShouldNotBeNil)

		_, _, err = cl.BridgeValues(realValue, emptyValue)
		So(err, ShouldNotBeNil)
	})

	Convey("Given a Macro Index with mixed tool usages", t, func() {
		macroIndex := macro.NewMacroIndexServer(
			macro.MacroIndexWithContext(ctx),
		)

		key25 := macro.AffineKeyFromValues(primitive.BaseValue(25), primitive.BaseValue(26))
		macroIndex.RecordOpcode(key25)

		key40 := macro.AffineKeyFromValues(primitive.BaseValue(40), primitive.BaseValue(41))
		for range 10 {
			macroIndex.RecordOpcode(key40)
		}

		Convey("GarbageCollect should prune inefficient logic circuits", func() {
			pruned := macroIndex.GarbageCollect()
			So(pruned, ShouldEqual, 1)

			_, found := macroIndex.FindOpcode(key25)
			So(found, ShouldBeFalse)

			op, found := macroIndex.FindOpcode(key40)
			So(found, ShouldBeTrue)
			So(op.Hardened, ShouldBeTrue)
			So(op.UseCount, ShouldEqual, 10)
		})
	})
}
