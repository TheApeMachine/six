package experiment

import (
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestValueReaction(t *testing.T) {
	Convey("Given the Destructive Interference engine", t, func() {
		// 1. Create the raw backend with a batch capacity of 2
		// This forces the backend to wait for both frames before reacting.
		backend := cpu.NewBackend(cpu.BackendWithBatchCap(2))

		Convey("When a Prompt collides with a Fact", func() {
			// 2. The Substrate Fact: "Roy is in the Kitchen"
			fact := primitive.NewValue()
			fact.Write([]byte("Roy is in the Kitchen"))
			fact.SetValueID(100)
			fact[primitive.StateSlotIndex] = 1 // Mark as active

			factFrame := make([]byte, primitive.ByteSize)
			primitive.ValueToBytes(fact, factFrame)

			// 3. The Prompt: "Roy"
			prompt := primitive.NewValue()
			prompt.Write([]byte("Roy"))
			prompt.SetValueID(200)
			prompt[primitive.StateSlotIndex] = 1 // Mark as active

			// 4. BEHAVIOR: Turn the Prompt into an active Instruction
			// We set the Instruction Flag to 1, and the Opcode to XOR (6)
			cpu.WriteRegion(prompt, cpu.RegionInstruction, 6)
			prompt[primitive.Words-1] |= primitive.InstructionMask

			promptFrame := make([]byte, primitive.ByteSize)
			primitive.ValueToBytes(prompt, promptFrame)

			// 5. Collide them in the Backend
			// The second write fills the batch and triggers processAvailableBatch()
			_, err1 := backend.Write(promptFrame)
			_, err2 := backend.Write(factFrame)

			So(err1, ShouldBeNil)
			So(err2, ShouldBeNil)

			// 6. Read the Residue (The Answer)
			residueFrame := make([]byte, primitive.ByteSize)
			n, err := backend.Read(residueFrame)

			So(err, ShouldBeNil)
			So(n, ShouldEqual, primitive.ByteSize)

			residue := primitive.BytesToValue(residueFrame)

			errnie.Trace(fmt.Sprintf("%v", residue.TokenIDs()))

			// 7. Verify the physics: "Roy" should be annihilated (0s), leaving the rest.
			// Token 0, 1, 2 ('R', 'o', 'y') should be 0 because they XOR'd themselves out.
			So(residue.TokenID(0), ShouldEqual, 0)
			So(residue.TokenID(1), ShouldEqual, 0)
			So(residue.TokenID(2), ShouldEqual, 0)

			// Token 3 should be ' ' (space)
			So(byte(residue.TokenID(3)>>32), ShouldEqual, ' ')
			// Token 4 should be 'i'
			So(byte(residue.TokenID(4)>>32), ShouldEqual, 'i')
			// Token 5 should be 's'
			So(byte(residue.TokenID(5)>>32), ShouldEqual, 's')

			t.Logf("Residue successfully computed. Shared tokens annihilated.")
		})
	})
}
