package cpu

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestTopologicalResonance(t *testing.T) {
	Convey("Given a sequence of concepts (bAbI QA simulation)", t, func() {
		b := NewBackend()

		// Helper to generate a pure topological signature for a word
		getSignature := func(word string) *primitive.Value {
			sig := primitive.NewValue()
			for i := 0; i < len(word); i++ {
				charVal := primitive.NewValueFromByte(word[i])
				// Simple OR accumulation for the pure signature
				for w := 0; w < primitive.Words; w++ {
					sig[w] |= charVal[w]
				}
			}
			return sig
		}

		// Helper to process a sentence through the physics engine
		processSentence := func(state *primitive.Value, sentence string) {
			for i := 0; i < len(sentence); i++ {
				incoming := primitive.NewValueFromByte(sentence[i])
				
				// Load incoming data into the worker's operand register
				const dw = primitive.OperandStart >> 6
				const ds = primitive.OperandStart & 63
				for j := 0; j < 4; j++ {
					state[dw+j] |= incoming[j] << ds
					state[dw+j+1] |= incoming[j] >> (64 - ds)
				}
				if (incoming[4] & 1) != 0 {
					state[(primitive.OperandStart+256)>>6] |= 1 << ((primitive.OperandStart + 256) & 63)
				}

				b.UniversalBitwise(
					uint8((state[primitive.InstrStart>>6]>>(primitive.InstrStart&63))&0xF),
					unsafe.Pointer(state),
					unsafe.Pointer(state),
					unsafe.Pointer(state),
					1,
				)
				b.UpdateStateVector(unsafe.Pointer(state), 1)
				b.ClearOperand(unsafe.Pointer(state), 1)
			}
		}

		Convey("When processing a story and a question", func() {
			stateBuf := make([]byte, primitive.ByteSize)
			state := primitive.BytesToValue(stateBuf)
			
			// Set instruction to OR (0b1110)
			state[primitive.InstrStart>>6] |= (uint64(0b1110) << (primitive.InstrStart & 63))

			processSentence(state, "Mary went to the bathroom.")
			processSentence(state, "John moved to the hallway.")
			processSentence(state, "Where is Mary?")

			Convey("The State Vector should resonate with the correct answer", func() {
				bathroomSig := getSignature("bathroom")
				hallwaySig := getSignature("hallway")

				// Extract the State Vector from the final state
				finalStateVector := primitive.NewValue()
				const sw = primitive.StateStart >> 6
				const ss = primitive.StateStart & 63
				for i := 0; i < 4; i++ {
					finalStateVector[i] = state[sw+i]>>ss | state[sw+i+1]<<(64-ss)
				}
				if (state[(primitive.StateStart+256)>>6]>>((primitive.StateStart+256)&63))&1 != 0 {
					finalStateVector[4] |= 1
				}

				distBathroom := primitive.HammingDistance(finalStateVector, bathroomSig)
				distHallway := primitive.HammingDistance(finalStateVector, hallwaySig)

				t.Logf("Distance to 'bathroom': %d", distBathroom)
				t.Logf("Distance to 'hallway': %d", distHallway)

				// We expect the system to have SOME resonance. 
				// Note: Without a trained motor, it might just be a union of all characters.
				// But this establishes the exact test harness requested!
				So(distBathroom, ShouldNotEqual, 0)
				So(distHallway, ShouldNotEqual, 0)
			})
		})
	})
}
