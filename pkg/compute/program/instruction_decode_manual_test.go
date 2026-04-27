package program

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestDecodeInstructionPredFields(t *testing.T) {
	Convey("Given a packed sweep instruction with compound predicate routing", t, func() {
		// Hand-built instruction word: predStart=3 and predCond=3 index the device
		// predicate table for Hamming/mask compound evaluation (see EncodeInstruction layout).
		const encodedInstructionWord = uint64(2414778202105355972)

		Convey("It should decode predicate start and condition fields", func() {
			_, _, _, _, _, _, _, _, _, predStart, predCond, _, _ := DecodeInstruction(encodedInstructionWord)
			So(predStart, ShouldEqual, 3)
			So(predCond, ShouldEqual, 3)
		})
	})
}

func BenchmarkDecodeInstructionPredFields(b *testing.B) {
	const encodedInstructionWord = uint64(2414778202105355972)

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_, _, _, _, _, _, _, _, _, _, _, _, _ = DecodeInstruction(encodedInstructionWord)
	}
}
