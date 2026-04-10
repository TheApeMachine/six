package programmer

import (
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
emitTransposedGPUProgramLayout is the shared Metal/CUDA path: opcode at the
program region, optional batch-affinity packing, pass count at word 124, then
transposed rotation banks from expandRotationsTransposed.
*/
func (compiler *Compiler) emitTransposedGPUProgramLayout(
	value *primitive.Value, intent Intent, useBatchAffinity bool,
) {
	opcode := intent.Operation

	if compiler.compileGeometricLayout(value, intent) {
		return
	}

	value.Set(
		core.Cfg.Value.Region.Program.Start,
		uint64(opcode),
	)

	if useBatchAffinity {
		applyBatchAffinityLayout(value, intent.Assets)

		return
	}

	passes := len(intent.Assets)

	if passes == 0 {
		passes = 1
	}

	/*
		Truth-table pass count for unified_bitwise; word 124 is also the batch
		candidate count slot for opcode 0x6 when batch affinity is off.
	*/
	value.Set(124, uint64(passes))

	cursor := 32

	for _, asset := range intent.Assets {
		cursor = compiler.expandRotationsTransposed(
			value, asset, cursor,
		)
	}
}
