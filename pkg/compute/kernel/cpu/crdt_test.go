package cpu

import (
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestCRDTSelfHealing(t *testing.T) {
	Convey("Given a distributed pipeline using the State Vector CRDT", t, func() {
		b := NewBackend()

		// We simulate a persistent worker state buffer
		stateBuf := make([]byte, primitive.ByteSize)
		state := primitive.BytesToValue(stateBuf)

		// Set instruction to OR (0b1110)
		state[primitive.InstrStart>>6] |= (uint64(0b1110) << (primitive.InstrStart & 63))

		// Helper to simulate the kernel pipeline processing a new incoming byte
		applyData := func(data byte) *primitive.Value {
			incoming := primitive.NewValueFromByte(data)

			// Load incoming data into the worker's operand register
			const dw = primitive.OperandStart >> 6
			const ds = primitive.OperandStart & 63
			for i := 0; i < 4; i++ {
				state[dw+i] |= incoming[i] << ds
				state[dw+i+1] |= incoming[i] >> (64 - ds)
			}
			if (incoming[4] & 1) != 0 {
				state[(primitive.OperandStart+256)>>6] |= 1 << ((primitive.OperandStart + 256) & 63)
			}

			// Run it through the kernel pipeline (ALU -> CRDT Fold -> Clear)
			// We just call UniversalBitwise, UpdateStateVector, ClearOperand directly
			// instead of Write to avoid the pipe blocking.
			b.UniversalBitwise(
				uint8((state[primitive.InstrStart>>6]>>(primitive.InstrStart&63))&0xF),
				unsafe.Pointer(state),
				unsafe.Pointer(state),
				unsafe.Pointer(state),
				1,
			)
			b.UpdateStateVector(unsafe.Pointer(state), 1)
			b.ClearOperand(unsafe.Pointer(state), 1)

			// Return a snapshot to simulate broadcasting it over UDP
			snapshot := primitive.NewValue()
			*snapshot = *state
			return snapshot
		}

		Convey("When the worker processes a stream of data", func() {
			// Worker processes 'A', 'B', 'C' and broadcasts each state
			packet1 := applyData('A')
			_ = applyData('B') // packet2 is dropped!
			packet3 := applyData('C')

			Convey("A receiving node can self-heal from dropped packets", func() {
				receiverState := primitive.NewValue()

				// Receiver gets packet 1
				primitive.MergeStateVector(receiverState, packet1)

				// Packet 2 is DROPPED over the network!
				// (We do nothing with packet2)

				// Receiver gets packet 3
				primitive.MergeStateVector(receiverState, packet3)

				// Prove that the receiver's State Vector contains the fingerprint for 'B'
				// even though packet 2 was completely lost.
				fingerprintB := primitive.NewValueFromByte('B')

				hasAllBits := true
				for i := 0; i < primitive.DataBits; i++ {
					if (fingerprintB[i>>6] & (1 << (i & 63))) != 0 {
						stateBitPos := primitive.StateStart + i
						if (receiverState[stateBitPos>>6] & (1 << (stateBitPos & 63))) == 0 {
							t.Logf("Missing bit %d in State Vector", i)
							hasAllBits = false
						}
					}
				}

				So(hasAllBits, ShouldBeTrue)
			})
		})
	})
}
