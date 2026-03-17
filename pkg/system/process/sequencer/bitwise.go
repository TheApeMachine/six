package sequencer

import (
	"bytes"
	"fmt"

	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/store/data"
)

/*
BitwiseHealer buffers fragmented sequencer output and heals false boundaries by
discovering exact shared spans through bitwise overlap.
Each byte is stored as a BaseValue so bitwise AND gives exact byte identity.
Fragments are groups of these per-byte values.
*/
type BitwiseHealer struct {
	state  *errnie.State
	buffer bytes.Buffer
	values [][]data.Value
}

/*
NewBitwiseHealer creates a fixed-capacity buffer for fragmented sequences.
*/
func NewBitwiseHealer() *BitwiseHealer {
	return &BitwiseHealer{
		state:  errnie.NewState("sequencer/bitwise/healer"),
		values: make([][]data.Value, 0),
	}
}

/*
Write appends a fragmented sequence to the buffer. Each fragment is a group
of per-byte BaseValues.
*/
func (bitwise *BitwiseHealer) Write(b byte, isBoundary bool) {
	bitwise.buffer.WriteByte(b)

	if isBoundary {
		chunk := make([]data.Value, 0, bitwise.buffer.Len())

		for _, b := range bitwise.buffer.Bytes() {
			chunk = append(chunk, data.BaseValue(b))
		}

		bitwise.values = append(bitwise.values, chunk)
		bitwise.buffer.Reset()
	}
}

/*
Heal splits the current fragments in half, ANDs the two halves' LCMs to get
the shared signal, sorts each chunk into left/overlap/right, and clears the
buffer for the next call.
*/
func (bitwise *BitwiseHealer) Heal() [][]byte {
	if len(bitwise.values) == 0 {
		return nil
	}

	if len(bitwise.values) == 1 {
		result := [][]byte{bitwise.decode(bitwise.values[0])}
		bitwise.values = bitwise.values[:0]

		return result
	}

	mid := len(bitwise.values) / 2
	halfA := bitwise.values[:mid]
	halfB := bitwise.values[mid:]

	lcmA := data.ValueLCM(bitwise.flatten(halfA))
	lcmB := data.ValueLCM(bitwise.flatten(halfB))
	shared := lcmA.AND(lcmB)

	var left [][]data.Value
	var overlap [][]data.Value
	var right [][]data.Value

	for _, chunk := range halfA {
		chunkLCM := data.ValueLCM(chunk)
		hit := chunkLCM.AND(shared)

		if hit.ActiveCount() == chunkLCM.ActiveCount() {
			overlap = append(overlap, chunk)
		} else {
			left = append(left, chunk)
		}
	}

	for _, chunk := range halfB {
		chunkLCM := data.ValueLCM(chunk)
		hit := chunkLCM.AND(shared)

		if hit.ActiveCount() == chunkLCM.ActiveCount() {
			overlap = append(overlap, chunk)
		} else {
			right = append(right, chunk)
		}
	}

	fmt.Println("--- left ---")
	for _, chunk := range left {
		fmt.Printf("  %q\n", string(bitwise.decode(chunk)))
	}

	fmt.Println("--- overlap ---")
	for _, chunk := range overlap {
		fmt.Printf("  %q\n", string(bitwise.decode(chunk)))
	}

	fmt.Println("--- right ---")
	for _, chunk := range right {
		fmt.Printf("  %q\n", string(bitwise.decode(chunk)))
	}

	bitwise.values = bitwise.values[:0]

	var result [][]byte

	for _, chunk := range left {
		result = append(result, bitwise.decode(chunk))
	}

	for _, chunk := range overlap {
		result = append(result, bitwise.decode(chunk))
	}

	for _, chunk := range right {
		result = append(result, bitwise.decode(chunk))
	}

	return result
}

/*
flatten concatenates multiple chunk slices into a single Value slice.
*/
func (bitwise *BitwiseHealer) flatten(chunks [][]data.Value) []data.Value {
	var result []data.Value

	for _, chunk := range chunks {
		result = append(result, chunk...)
	}

	return result
}

/*
decode converts a slice of per-byte BaseValues back to a byte slice.
*/
func (bitwise *BitwiseHealer) decode(values []data.Value) []byte {
	result := make([]byte, len(values))

	for idx, value := range values {
		for symbol := range 256 {
			if bitwise.matchBytes(value, data.BaseValue(byte(symbol))) {
				result[idx] = byte(symbol)
				break
			}
		}
	}

	return result
}

/*
matchBytes checks if two byte-Values represent the same byte via AND.
*/
func (bitwise *BitwiseHealer) matchBytes(valA, valB data.Value) bool {
	shared := valA.AND(valB)

	return shared.ActiveCount() == valA.ActiveCount() &&
		shared.ActiveCount() == valB.ActiveCount()
}
