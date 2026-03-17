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
Flush emits any remaining bytes in the buffer as a final chunk.
Call after feeding all input when the stream ends.
*/
func (bitwise *BitwiseHealer) Flush() [][]byte {
	if bitwise.buffer.Len() == 0 {
		return nil
	}

	chunk := make([]data.Value, 0, bitwise.buffer.Len())

	for _, b := range bitwise.buffer.Bytes() {
		chunk = append(chunk, data.BaseValue(b))
	}

	bitwise.values = append(bitwise.values, chunk)
	bitwise.buffer.Reset()

	return bitwise.Heal()
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

	fmt.Println("--- starting iterative heal ---")

	// Phase 1: Determine the "shared" anchor signal across the sequences
	// by checking what the two halves of the buffer have in common.
	mid := len(bitwise.values) / 2
	halfA := bitwise.values[:mid]
	halfB := bitwise.values[mid:]

	lcmA := data.ValueLCM(bitwise.flatten(halfA))
	lcmB := data.ValueLCM(bitwise.flatten(halfB))
	shared := lcmA.AND(lcmB)

	// Phase 2: Iteratively merge adjacent chunks that belong to the SAME category
	// Category is defined as: chunk is entirely composed of bits present in the shared anchor.
	changed := true
	for changed {
		changed = false
		var next [][]data.Value

		for i := 0; i < len(bitwise.values); i++ {
			if i == len(bitwise.values)-1 {
				next = append(next, bitwise.values[i])
				break
			}

			curr := bitwise.values[i]
			nxt := bitwise.values[i+1]

			currLCM := data.ValueLCM(curr)
			nxtLCM := data.ValueLCM(nxt)

			currHit := currLCM.AND(shared)
			nxtHit := nxtLCM.AND(shared)

			currIsShared := currHit.ActiveCount() == currLCM.ActiveCount()
			nxtIsShared := nxtHit.ActiveCount() == nxtLCM.ActiveCount()

			if currIsShared == nxtIsShared {
				fmt.Printf("  Merging %q and %q (Category match: %v)\n", string(bitwise.decode(curr)), string(bitwise.decode(nxt)), currIsShared)
				
				merged := append([]data.Value{}, curr...)
				merged = append(merged, nxt...)
				next = append(next, merged)
				
				// skip next
				next = append(next, bitwise.values[i+2:]...)
				changed = true
				break
			} else {
				next = append(next, curr)
			}
		}
		bitwise.values = next
	}

	var result [][]byte
	for _, chunk := range bitwise.values {
		result = append(result, bitwise.decode(chunk))
	}

	bitwise.values = bitwise.values[:0]
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
