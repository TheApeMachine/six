package experiment

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestValueReaction(t *testing.T) {
	Convey("Given the Graph Reduction Engine", t, func() {
		backend := cpu.NewBackend(cpu.BackendWithBatchCap(2))

		Convey("When executing the 'Where is Roy?' graph traversal", func() {
			// ---------------------------------------------------------
			// 1. BUILD THE GRAPH (From your diagram)
			// ---------------------------------------------------------

			// Node A: [Kitchen]
			kitchen := primitive.NewValue()
			kitchen.Write([]byte("Kitchen"))
			kitchen.SetValueID(300)
			kitchen[primitive.StateSlotIndex] = 1

			// Node B: [Roy] -points to-> [Kitchen]
			royFact := primitive.NewValue()
			royFact.Write([]byte("Roy"))
			royFact.SetValueID(200)
			royFact.SetNextValueID(kitchen.ValueID()) // The Arrow!
			royFact[primitive.StateSlotIndex] = 1

			// ---------------------------------------------------------
			// 2. THE PROMPT (The Executable Query)
			// ---------------------------------------------------------

			// Prompt: [Roy] (Instruction = XOR)
			prompt := primitive.NewValue()
			prompt.Write([]byte("Roy"))
			prompt.SetValueID(999)
			prompt[primitive.StateSlotIndex] = 1

			// Turn it into an active instruction
			cpu.WriteRegion(prompt, cpu.RegionInstruction, 6) // 6 = XOR
			prompt[primitive.Words-1] |= primitive.InstructionMask

			// ---------------------------------------------------------
			// 3. THE COLLISION (Values passing through Values)
			// ---------------------------------------------------------

			promptFrame := make([]byte, primitive.ByteSize)
			primitive.ValueToBytes(prompt, promptFrame)

			factFrame := make([]byte, primitive.ByteSize)
			primitive.ValueToBytes(royFact, factFrame)

			// Push to backend. Prompt goes first so it acts as the instruction.
			backend.Write(promptFrame)
			backend.Write(factFrame)

			// ---------------------------------------------------------
			// 4. THE RESULT
			// ---------------------------------------------------------

			residueFrame := make([]byte, primitive.ByteSize)
			n, err := backend.Read(residueFrame)
			So(err, ShouldBeNil)
			So(n, ShouldEqual, primitive.ByteSize)

			residue := primitive.BytesToValue(residueFrame)

			// PHYSICS CHECK 1: Destructive Interference
			// "Roy" XOR "Roy" = 0. The data is annihilated.
			So(residue.TokenID(0), ShouldEqual, 0)
			So(residue.TokenID(1), ShouldEqual, 0)
			So(residue.TokenID(2), ShouldEqual, 0)

			// PHYSICS CHECK 2: The Instruction Pointer
			// The residue must now point to [Kitchen] (ID 300)
			So(residue.NextValueID(), ShouldEqual, 300)

			t.Logf("Graph Traversal Success!")
			t.Logf("Data annihilated to 0. Instruction pointer advanced to Node ID: %d (Kitchen)", residue.NextValueID())
		})
	})
}
