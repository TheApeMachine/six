package programmer

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
compileMetal arranges the Value for Apple Metal
GPU execution.

Metal dispatches one threadgroup per Value, 16
threads per threadgroup — one per rotation. Adjacent
threads should read adjacent memory for coalesced
access within a SIMD group.

The CPU layout packs each rotation's 4 words
contiguously: [rot0_w0, rot0_w1, rot0_w2, rot0_w3,
rot1_w0, ...]. Thread 0 reads word 32, thread 1
reads word 36 — stride of 4, not coalesced.

Metal layout transposes this. All word-0s together,
all word-1s together:

	words 32-47:  word 0 of rotations 0-15
	words 48-63:  word 1 of rotations 0-15
	words 64-79:  word 2 of rotations 0-15
	words 80-95:  word 3 of rotations 0-15

Now thread i reads words 32+i, 48+i, 64+i, 80+i.
Adjacent threads read adjacent memory. Coalesced.

The kernel becomes:

	tid = thread_index_in_threadgroup  // 0-15
	a0 = value[0]; a1 = value[1]; a2 = value[2]; a3 = value[3]
	b0 = value[32 + tid]
	b1 = value[48 + tid]
	b2 = value[64 + tid]
	b3 = value[80 + tid]
	signal = a0 op b0, a1 op b1, a2 op b2, a3 op b3
	value[24 + tid/8] |= extract_byte(signal) << ((tid%8)*8)
*/
func (compiler *Compiler) Metal(
	value *primitive.Value, intent Intent, useBatchAffinity bool,
) {
	compiler.emitTransposedGPUProgramLayout(value, intent, useBatchAffinity)
}

/*
expandRotationsTransposed pre-computes all 16
rotations and writes them in transposed order:
all word-0s first, then all word-1s, etc. This
gives coalesced reads when each GPU thread handles
one rotation.

Uses 64 words of reserved space per asset (16
rotations × 4 words, just reordered).
*/
func (compiler *Compiler) expandRotationsTransposed(
	value *primitive.Value, source []uint64, cursor int,
) int {
	var span [4]uint64

	for i := 0; i < 4 && i < len(source); i++ {
		span[i] = source[i]
	}

	// Pre-compute all 16 rotations.
	var rotations [16][4]uint64

	for rot := range 16 {
		rotations[rot] = span

		span[0] = bits.RotateLeft64(span[0], 8)
		span[1] = bits.RotateLeft64(span[1], 8)
		span[2] = bits.RotateLeft64(span[2], 8)
		span[3] = bits.RotateLeft64(span[3], 8)
	}

	// Write transposed: all word-0s, then word-1s, etc.
	for word := range 4 {
		for rot := range 16 {
			if cursor > 124 {
				return cursor
			}

			value.Set(cursor, rotations[rot][word])
			cursor++
		}
	}

	return cursor
}
