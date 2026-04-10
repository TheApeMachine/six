package programmer

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
CPU arranges the Value for NEON (ARM64) or AVX2 (AMD64) execution.

NEON has 32 × 128-bit registers. A (words 0-3) takes 2 registers
and stays pinned. Each B rotation is 32 bytes = 2 registers. That
leaves 28 registers for B rotations — 14 rotations simultaneously.

Layout:

	word   8:     opcode (low 4 bits)
	word  124:    pass count (multi-asset)
	words 32-95:  B rotations (16 rotations × 4 words)

The kernel loads A once, then sweeps through the rotations in
batches. Each batch is a straight SIMD pass — no branching, no
rotation instruction, just load-op-store.
*/
func (compiler *Compiler) CPU(
	value *primitive.Value, intent Intent, useBatchAffinity bool,
) {
	opcode := intent.Operation

	value.Set(core.Cfg.Value.Region.Program.Start, uint64(opcode))

	if useBatchAffinity {
		applyBatchAffinityLayout(value, intent.Assets)

		return
	}

	passes := len(intent.Assets)

	if passes == 0 {
		passes = 1
	}

	// Pass count at word 124, not word 32 — word 32 is the
	// start of rotation data and must not be clobbered.
	value.Set(124, uint64(passes))

	cursor := 32

	for _, asset := range intent.Assets {
		cursor = expandRotations(value, asset, cursor)
	}
}

/*
expandRotations pre-computes all 16 byte-aligned rotations of a
4-word (256-bit) span and writes them contiguously starting at the
given cursor position in the Value.

Returns the new cursor position after the last written word. The
boundary is word 124, which holds pass metadata.
*/
func expandRotations(
	value *primitive.Value, source []uint64, cursor int,
) int {
	var span [4]uint64

	for idx := 0; idx < 4 && idx < len(source); idx++ {
		span[idx] = source[idx]
	}

	for range 16 {
		if cursor+4 > 124 {
			break
		}

		value.Set(cursor, span[0])
		value.Set(cursor+1, span[1])
		value.Set(cursor+2, span[2])
		value.Set(cursor+3, span[3])

		cursor += 4

		span[0] = bits.RotateLeft64(span[0], 8)
		span[1] = bits.RotateLeft64(span[1], 8)
		span[2] = bits.RotateLeft64(span[2], 8)
		span[3] = bits.RotateLeft64(span[3], 8)
	}

	return cursor
}
