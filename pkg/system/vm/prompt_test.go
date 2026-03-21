package vm

import (
	"context"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/logic/lang/primitive"
	"github.com/theapemachine/six/pkg/logic/synthesis/bvp"
	"github.com/theapemachine/six/pkg/store/data/provider/local"
	"github.com/theapemachine/six/pkg/system/cluster"
)

/*
TestMachinePromptExactContinuation verifies that the machine returns the exact
continuation for one ingested corpus line through the real tokenizer and graph.
*/
func TestMachinePromptExactContinuation(t *testing.T) {
	gc.Convey("Given a machine with one ingested exact corpus line", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine := NewMachine(
			MachineWithContext(ctx),
		)
		defer machine.Close()

		err := machine.SetDataset(
			local.New(local.WithStrings([]string{
				"Roy is in the Kitchen",
			})),
		)
		gc.So(err, gc.ShouldBeNil)
		gc.So(machine.booter.cantilever.Load(), gc.ShouldBeGreaterThan, 0)

		before := machine.workerPool.Metrics().JobCount

		gc.Convey("It should schedule prompt execution through the worker pool", func() {
			result, err := machine.Prompt("Roy is in the ")

			gc.So(err, gc.ShouldBeNil)
			gc.So(string(result), gc.ShouldEqual, "Kitchen")
			gc.So(machine.workerPool.Metrics().JobCount, gc.ShouldBeGreaterThan, before)
		})
	})

	gc.Convey("Given a machine with one ingested exact corpus line", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine := NewMachine(
			MachineWithContext(ctx),
		)
		defer machine.Close()

		err := machine.SetDataset(
			local.New(local.WithStrings([]string{
				"Roy is in the Kitchen",
			})),
		)
		gc.So(err, gc.ShouldBeNil)

		gc.Convey("It should return the exact continuation through the graph", func() {
			result, err := machine.Prompt("Roy is in the ")

			gc.So(err, gc.ShouldBeNil)
			gc.So(string(result), gc.ShouldEqual, "Kitchen")
		})
	})

	gc.Convey("Given a machine with multiple ingested corpus lines", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine := NewMachine(
			MachineWithContext(ctx),
		)
		defer machine.Close()

		err := machine.SetDataset(
			local.New(local.WithStrings([]string{
				"Roy was in the living room.",
				"Roy is in the kitchen.",
				"If you have an umbrella or stay inside you stay dry.",
			})),
		)
		gc.So(err, gc.ShouldBeNil)

		gc.Convey("It should stop at the matched sequence boundary", func() {
			result, err := machine.Prompt("Roy was in the ")

			gc.So(err, gc.ShouldBeNil)
			gc.So(string(result), gc.ShouldEqual, "living room.")
		})

		gc.Convey("It should cut the prompt using an exact prefix, not a later repeated token", func() {
			result, err := machine.Prompt("If you have an umbrella or ")

			gc.So(err, gc.ShouldBeNil)
			gc.So(string(result), gc.ShouldEqual, "stay inside you stay dry.")
		})
	})

	cases := []struct {
		name     string
		prompt   string
		expected string
	}{
		{
			name:     "Roy is in the Kitchen",
			prompt:   "Roy is in the ",
			expected: "Kitchen",
		},
		{
			name:     "Sandra is in the Garden",
			prompt:   "Sandra is in the ",
			expected: "Garden",
		},
		{
			name:     "Guinevere is in the Library",
			prompt:   "Guinevere is in the ",
			expected: "Library",
		},
		{
			name:     "Christobal is in the Mental Institution",
			prompt:   "Christobal is in the ",
			expected: "Mental Institution",
		},
		{
			name:     "Yo mama so fat she needs a GPS to find her way to the kitchen",
			prompt:   "Yo mama so fat",
			expected: "she needs a GPS to find her way to the kitchen",
		},
	}

	for _, c := range cases {
		gc.Convey("Given "+c.name, t, func() {
			machine := NewMachine(
				MachineWithContext(t.Context()),
			)

			defer machine.Close()

			err := machine.SetDataset(
				local.New(local.WithStrings([]string{
					c.prompt + c.expected,
				})),
			)

			gc.Convey(c.name+" should return the exact continuation through the graph", func() {
				gc.So(err, gc.ShouldBeNil)

				result, err := machine.Prompt(c.prompt)
				gc.So(err, gc.ShouldBeNil)
				gc.So(string(result), gc.ShouldEqual, c.expected)
			})
		})
	}
}

func TestCantileverPromptViaRouter(t *testing.T) {
	gc.Convey("Given a machine with one ingested exact corpus line", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		machine := NewMachine(
			MachineWithContext(ctx),
		)
		defer machine.Close()

		err := machine.SetDataset(
			local.New(local.WithStrings([]string{
				"Roy is in the Kitchen",
			})),
		)
		gc.So(err, gc.ShouldBeNil)

		row, err := machine.compilePromptRow([]byte("Roy is in the Kitchen"))
		gc.So(err, gc.ShouldBeNil)
		gc.So(len(row), gc.ShouldBeGreaterThan, 0)
		machine.booter.cantilever.Store([][]primitive.Value{row})

		gc.Convey("It should return the continuation through the cantilever capability", func() {
			raw, err := machine.booter.router.Get(ctx, cluster.CANTILEVER, "test")
			gc.So(err, gc.ShouldBeNil)
			defer raw.Release()

			future, release := bvp.Cantilever(raw).Prompt(
				ctx, func(params bvp.Cantilever_prompt_Params) error {
					return params.SetMsg("Roy is in the ")
				},
			)
			defer release()

			result, err := future.Struct()
			gc.So(err, gc.ShouldBeNil)

			text, err := result.Result()
			gc.So(err, gc.ShouldBeNil)
			gc.So(text, gc.ShouldEqual, "Kitchen")
		})
	})
}

/*
BenchmarkMachinePromptExactContinuation measures one exact machine prompt.
*/
func BenchmarkMachinePromptExactContinuation(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	machine := NewMachine(
		MachineWithContext(ctx),
	)
	defer machine.Close()

	if err := machine.SetDataset(
		local.New(local.WithStrings([]string{
			"Roy is in the Kitchen",
		})),
	); err != nil {
		b.Fatal(err)
	}

	for b.Loop() {
		result, err := machine.Prompt("Roy is in the ")
		if err != nil {
			b.Fatal(err)
		}

		if string(result) != "Kitchen" {
			b.Fatalf("expected Kitchen, got %q", string(result))
		}
	}
}
