package vm

import (
	"context"
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/store/data/provider/local"
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

		gc.Convey("It should return the exact continuation through the graph", func() {
			result, err := machine.Prompt("Roy is in the ")

			gc.So(err, gc.ShouldBeNil)
			gc.So(string(result), gc.ShouldEqual, "Kitchen")
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

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		result, err := machine.Prompt("Roy is in the ")
		if err != nil {
			b.Fatal(err)
		}

		if string(result) != "Kitchen" {
			b.Fatalf("expected Kitchen, got %q", string(result))
		}
	}
}
