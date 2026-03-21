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

func newCantileverRouter(
	ctx context.Context,
	includeTokenizer bool,
	includeMacro bool,
) (*cluster.Router, *macro.MacroIndexServer, *tokenizer.UniversalServer, *input.PrompterServer) {
	router := cluster.NewRouter(cluster.RouterWithContext(ctx))

	var tok *tokenizer.UniversalServer
	var prompter *input.PrompterServer
	if includeTokenizer {
		tok = tokenizer.NewUniversalServer(
			tokenizer.UniversalWithContext(ctx),
		)
		router.Register(cluster.TOKENIZER, tok)

		prompter = input.NewPrompterServer(
			input.PrompterWithContext(ctx),
		)
		router.Register(cluster.PROMPTER, prompter)
	}

	var index *macro.MacroIndexServer
	if includeMacro {
		index = macro.NewMacroIndexServer(
			macro.MacroIndexWithContext(ctx),
		)
		router.Register(cluster.MACROINDEX, index)
	}

	return router, index, tok, prompter
}

func TestCantileverPromptValues(t *testing.T) {
	Convey("Given a cantilever server with tokenizer and prompter capabilities", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		router, _, tok, prompter := newCantileverRouter(ctx, true, false)
		defer router.Close()
		defer tok.Close()
		defer prompter.Close()

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

func observableValues(text string) []primitive.Value {
	values := make([]primitive.Value, 0, len(text))

	for _, symbol := range []byte(text) {
		value := primitive.SeedObservable(symbol, primitive.NeutralValue())
		values = append(values, value)
	}

	return values
}

func TestCantileverOperatorContinuationLeadIndex(t *testing.T) {
	Convey("Given a cantilever server with stored corpus rows", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		router, macroIndex, _, _ := newCantileverRouter(ctx, false, true)
		defer router.Close()
		defer macroIndex.Close()

		server := NewCantileverServer(
			CantileverWithContext(ctx),
			CantileverWithRouter(router),
		)

		royRow := observableValues("Roy is in the Kitchen")
		aliceRow := observableValues("Alice is in the Garden")

		server.Store([][]primitive.Value{aliceRow, royRow})

		Convey("It should return nil when no lead-symbol candidate exists", func() {
			noLeadPrompt := observableValues("Zed is in ")
			continuation := server.operatorContinuation(noLeadPrompt)
			So(continuation, ShouldBeNil)
		})

		Convey("It should bridge using rows matching the prompt lead symbol", func() {
			prompt := observableValues("Roy is in xx")
			continuation := server.operatorContinuation(prompt)
			So(string(decodePromptValues(continuation)), ShouldEqual, "he Kitchen")
		})
	})
}

func BenchmarkCantileverPromptValues(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	router, _, tok, prompter := newCantileverRouter(ctx, true, false)
	defer router.Close()
	defer tok.Close()
	defer prompter.Close()

	server := NewCantileverServer(
		CantileverWithContext(ctx),
		CantileverWithRouter(router),
	)

	client := tokenizer.Universal(tok.Client("bench"))
	for _, symbol := range []byte("Roy is in the Kitchen") {
		if err := client.Write(ctx, func(params tokenizer.Universal_write_Params) error {
			params.SetData(symbol)
			return nil
		}); err != nil {
			b.Fatalf("write: %v", err)
		}
	}

	if err := client.WaitStreaming(); err != nil {
		b.Fatalf("wait streaming: %v", err)
	}

	keys, err := server.tokenizerKeys(ctx, client)
	if err != nil {
		b.Fatalf("tokenizer keys: %v", err)
	}

	server.Store([][]primitive.Value{
		primitive.CompileObservableSequenceValues(keys),
	})

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		promptValues, err := server.promptValues(ctx, []byte("Roy is in the "))
		if err != nil {
			b.Fatalf("prompt values: %v", err)
		}

		continuation := server.exactContinuation(promptValues)
		if got := string(decodePromptValues(continuation)); got != "Kitchen" {
			b.Fatalf("continuation = %q, want %q", got, "Kitchen")
		}
	}
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
			router, macroIndex, _, _ := newCantileverRouter(ctx, false, true)
			defer router.Close()
			defer macroIndex.Close()

			startValue := primitive.BaseValue(tc.StartByte)
			goalValue := primitive.BaseValue(tc.GoalByte)

			cl := NewCantileverServer(
				CantileverWithContext(ctx),
				CantileverWithRouter(router),
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
		router, macroIndex, _, _ := newCantileverRouter(ctx, false, true)
		defer router.Close()
		defer macroIndex.Close()

		cl := NewCantileverServer(
			CantileverWithContext(ctx),
			CantileverWithRouter(router),
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

func TestCantileverRouterFailures(t *testing.T) {
	Convey("Given a cantilever server without a router", t, func() {
		server := NewCantileverServer(
			CantileverWithContext(context.Background()),
		)

		_, _, err := server.BridgeValues(primitive.BaseValue('A'), primitive.BaseValue('B'))
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, string(ErrCantileverRouterRequired))

		_, err = server.promptValues(context.Background(), []byte("Roy"))
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, string(ErrCantileverRouterRequired))
	})

	Convey("Given a cantilever server with router but no macro index capability", t, func() {
		router := cluster.NewRouter(cluster.RouterWithContext(context.Background()))
		defer router.Close()

		server := NewCantileverServer(
			CantileverWithContext(context.Background()),
			CantileverWithRouter(router),
		)

		_, _, err := server.BridgeValues(primitive.BaseValue('A'), primitive.BaseValue('B'))
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "router: no service registered for")
	})

	Convey("Given a cantilever server with router but no tokenizer capability", t, func() {
		router, macroIndex, _, _ := newCantileverRouter(context.Background(), false, true)
		defer router.Close()
		defer macroIndex.Close()

		server := NewCantileverServer(
			CantileverWithContext(context.Background()),
			CantileverWithRouter(router),
		)

		_, err := server.promptValues(context.Background(), []byte("Roy"))
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "router: no service registered for")
	})
}
