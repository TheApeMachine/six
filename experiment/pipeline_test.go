package experiment

import (
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/experiment/data/huggingface"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestValueReaction(t *testing.T) {
	Convey("Given a Dataset", t, func() {
		dataset := huggingface.New(
			huggingface.DatasetWithContext(t.Context()),
			huggingface.DatasetWithRepo("facebook/babi_qa"),
			huggingface.DatasetWithSubset("en-10k-qa1"),
			huggingface.DatasetWithTextColumns("story"),
		)

		Convey("The Substrate computes inherently via feedback", func() {
			backend := cpu.NewBackend()

			chamber := primitive.NewValue()
			frame := make([]byte, primitive.ByteSize)
			var sawEmergent bool

			for i := range 10 {
				_, err := io.ReadFull(dataset, frame)
				So(err, ShouldBeNil)

				incoming := primitive.NewValue()
				_, err = incoming.Write(frame)
				So(err, ShouldBeNil)

				_, err = io.Copy(chamber, incoming)
				So(err, ShouldBeNil)

				_, err = io.Copy(backend, chamber)
				So(err, ShouldBeNil)

				mutatedFrame := make([]byte, primitive.ByteSize)
				_, err = io.ReadFull(backend, mutatedFrame)
				So(err, ShouldBeNil)

				chamber = primitive.BytesToValue(mutatedFrame)

				instr := cpu.ReadRegion(chamber, cpu.RegionInstruction) & 0xF
				pressure := cpu.Popcount(chamber, primitive.AccumStart, primitive.AccumBits)
				if pressure > 0 || instr != 0 {
					sawEmergent = true
				}

				t.Logf("Iteration %d - Instruction Evolved to: %04b, Pressure: %d",
					i, instr, pressure)
			}

			So(sawEmergent, ShouldBeTrue)
		})
	})
}
