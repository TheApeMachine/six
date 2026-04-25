package cpu

import (
	"math"
	"testing"
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
)

func TestGeometricFrame(t *testing.T) {
	opcodes := []uint64{0x10, 0x20, 0x30}

	for _, opcode := range opcodes {
		t.Run("opcode", func(t *testing.T) {
			reference := geometricFixture()
			actual := geometricFixture()

			if !geometricFrameGeneric(unsafe.Pointer(&reference), opcode) {
				t.Fatalf("generic lane rejected opcode %x", opcode)
			}

			if !GeometricFrame(unsafe.Pointer(&actual), opcode) {
				t.Fatalf("native lane rejected opcode %x", opcode)
			}

			for word := 32; word < 40; word++ {
				if reference[word] != actual[word] {
					t.Fatalf("signals word %d mismatch: reference=%016x actual=%016x", word, reference[word], actual[word])
				}
			}
		})
	}
}

func geometricFixture() primitive.Value {
	var value primitive.Value

	for lane := 0; lane < 8; lane++ {
		left := float64(lane+1) * 0.25
		right := float64((lane+3)*(lane+5)) * 0.125

		value[40+lane] = math.Float64bits(left)
		value[48+lane] = math.Float64bits(right)
	}

	return value
}
