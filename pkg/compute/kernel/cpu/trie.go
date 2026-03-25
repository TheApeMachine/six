package cpu

import (
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
)

// AffinityMatch returns true if two Values should be paired for ALU processing
// based on their affinity masks (Region 2). This replaces the old token-based
// longestCancellationSpan logic.
func AffinityMatch(a, b *primitive.Value, threshold int) bool {
	affA := a.AffinityMask()
	affB := b.AffinityMask()
	overlap := affA & affB
	// Use the existing Popcount but on a temporary value containing the overlap
	tmp := primitive.Value{}
	tmp[0] = overlap
	return Popcount(&tmp, 0, 64) > threshold
}

func decodeBatchFrames(batch []byte) []primitive.Value {
	count := len(batch) / primitive.ByteSize
	values := make([]primitive.Value, 0, count)

	for offset := 0; offset+primitive.ByteSize <= len(batch); offset += primitive.ByteSize {
		frame := primitive.BytesToValue(batch[offset : offset+primitive.ByteSize])
		values = append(values, *frame)
	}

	return values
}

/*
EncodeTrieBatch packs byte sequences into Region0 TokenIDs and IDs.
This is still useful for testing and initialization.
*/
func (backend *Backend) EncodeTrieBatch(
	sequences [][]byte, dst unsafe.Pointer, numValues uint32,
) {
	ds := unsafe.Slice((*[primitive.Words]uint64)(dst), numValues)

	for v := uint32(0); v < numValues; v++ {
		value := (*primitive.Value)(unsafe.Pointer(&ds[v]))
		seq := sequences[v]

		for i, b := range seq {
			if i >= primitive.Region0TokenCount {
				break
			}
			value.SetTokenID(i, primitive.Tokenize(b, uint64(i)))
		}

		value.SetValueID(uint64(v + 1))
	}
}
