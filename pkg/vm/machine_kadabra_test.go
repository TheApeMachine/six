package vm

import (
	"bytes"
	"io"
	"iter"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/local"
	"github.com/theapemachine/six/pkg/store/kadabra"
)

type promptDataset struct {
	prompts []data.Prompt
	closed  bool
}

func (dataset *promptDataset) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (dataset *promptDataset) Close() error {
	dataset.closed = true
	return nil
}

func (dataset *promptDataset) Generate() iter.Seq[byte] {
	return func(yield func(byte) bool) {
		for _, prompt := range dataset.prompts {
			for _, symbol := range []byte(prompt.Text) {
				if !yield(symbol) {
					return
				}
			}
		}
	}
}

func (dataset *promptDataset) GeneratePrompts() iter.Seq[data.Prompt] {
	return func(yield func(data.Prompt) bool) {
		for _, prompt := range dataset.prompts {
			if !yield(prompt) {
				return
			}
		}
	}
}

func TestMachineRunPublishesPromptSequences(t *testing.T) {
	Convey("Given a machine with a prompt-aware dataset", t, func() {
		dataset := &promptDataset{
			prompts: []data.Prompt{
				{
					Text:     "blue cab",
					Label:    "Truck",
					HasLabel: true,
				},
				{
					Text: "red cab",
				},
			},
		}
		node := kadabra.NewKadabraNode(7)

		machine, err := NewMachine(
			t.Context(),
			MachineWithDataset(dataset),
			MachineWithKadabraNode(node),
			MachineWithKadabraLabel("Corpus"),
		)

		Convey("Run should publish prompt sequences into Kadabra with labels preserved", func() {
			So(err, ShouldBeNil)
			So(machine.Run(), ShouldBeNil)
			So(dataset.closed, ShouldBeTrue)
			So(node.Store.CurrentStep(), ShouldEqual, 2)
			So(node.HasRecord(kadabra.HashSequenceRecord("blue cab", "Truck")), ShouldBeTrue)
			So(node.HasRecord(kadabra.HashSequenceRecord("red cab", "Corpus")), ShouldBeTrue)

			blueScores := node.Store.Classify("blue cab")
			redScores := node.Store.Classify("red cab")

			So(blueScores["Truck"], ShouldBeGreaterThan, blueScores["Corpus"])
			So(redScores["Corpus"], ShouldBeGreaterThan, redScores["Truck"])
		})
	})
}

func TestMachineRunPublishesBufferedChunks(t *testing.T) {
	Convey("Given a machine with a raw byte dataset", t, func() {
		payload := bytes.Repeat([]byte("a"), defaultMachineChunkBytes+6)
		node := kadabra.NewKadabraNode(11)

		machine, err := NewMachine(
			t.Context(),
			MachineWithDataset(local.New(local.WithBytes(payload))),
			MachineWithKadabraNode(node),
		)

		Convey("Run should publish both full and trailing chunks into Kadabra", func() {
			So(err, ShouldBeNil)
			So(machine.Run(), ShouldBeNil)
			So(node.Store.CurrentStep(), ShouldEqual, 2)

			firstChunk := string(payload[:defaultMachineChunkBytes])
			lastChunk := string(payload[defaultMachineChunkBytes:])

			So(node.HasRecord(kadabra.HashSequenceRecord(firstChunk, defaultMachineLabel)), ShouldBeTrue)
			So(node.HasRecord(kadabra.HashSequenceRecord(lastChunk, defaultMachineLabel)), ShouldBeTrue)
		})
	})
}

func BenchmarkMachineRun(b *testing.B) {
	payload := bytes.Repeat([]byte("a"), defaultMachineChunkBytes*32)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		node := kadabra.NewKadabraNode(19)
		machine, err := NewMachine(
			b.Context(),
			MachineWithDataset(local.New(local.WithBytes(payload))),
			MachineWithKadabraNode(node),
		)

		if err != nil {
			b.Fatal(err)
		}

		if err := machine.Run(); err != nil {
			b.Fatal(err)
		}
	}
}
