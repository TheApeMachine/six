package programmer

import (
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
compileCUDA arranges the Value for NVIDIA CUDA
execution.

CUDA warps are 32 threads wide. With 16 rotations
per Value, half the warp is idle — or the kernel
pairs two Values per warp. Either way, coalesced
memory access within the warp matters most.

The layout is transposed, same principle as Metal:

	words 32-47:  word 0 of rotations 0-15
	words 48-63:  word 1 of rotations 0-15
	words 64-79:  word 2 of rotations 0-15
	words 80-95:  word 3 of rotations 0-15

Adjacent threads read adjacent uint64s. A 128-byte
cache line covers 16 × uint64 — exactly one
rotation-word bank. One memory transaction loads
the entire bank for all 16 threads.

If the kernel pairs two Values per warp, the second
Value's rotations start at word 96 (or the kernel
maps threads 16-31 to a second Value in the batch).
The programmer doesn't need to care — it just
optimizes the single Value layout. The kernel
handles batching.
*/
func (compiler *Compiler) CUDA(
	value *primitive.Value, intent Intent, useBatchAffinity bool,
) {
	opcode := intent.Operation

	value.Set(
		core.Cfg.Value.Region.Program.Start,
		uint64(opcode),
	)

	if useBatchAffinity {
		packed := packNearestAffinityCandidates(value, intent.Assets)

		if packed == 0 {
			for wordIdx := range primitive.AffinityWords {
				value.Set(32+wordIdx, 0)
			}

			packed = 1
		}

		value.Set(124, uint64(packed))

		return
	}

	passes := len(intent.Assets)

	if passes == 0 {
		passes = 1
	}

	value.Set(124, uint64(passes))

	cursor := 32

	for _, asset := range intent.Assets {
		cursor = compiler.expandRotationsTransposed(
			value, asset, cursor,
		)
	}
}
