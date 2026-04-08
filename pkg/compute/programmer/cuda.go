package programmer

import "github.com/theapemachine/six/pkg/primitive"

/*
CUDA arranges the Value for NVIDIA CUDA execution using the same transposed
rotation layout as Metal; see emitTransposedGPUProgramLayout.
*/
func (compiler *Compiler) CUDA(
	value *primitive.Value, intent Intent, useBatchAffinity bool,
) {
	compiler.emitTransposedGPUProgramLayout(value, intent, useBatchAffinity)
}
