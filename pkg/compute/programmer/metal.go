package programmer

/*
MetalCompiler selects the Metal CompilerTarget when lowering tokens to Frames.

Today the frame builder is target-agnostic: newFrameBuilder(...).frames(Metal)
runs the same packTruth path as CPU and CUDA (see frameBuilder.packTruth, which
ignores the target). The Metal tag exists so kernel.Substrate and future
lowering can branch without changing the parser—e.g. specializing packed
program words for SIMD subgroup width, threadgroup-sized batches, or memory
layout for constant-address argument buffers. No MSL is generated here; GPU
work grouping, resource binding, and synchronization live in the Metal
substrate implementation, not in this package.

Assumption: Metal-capable devices match whatever the linked kernel package
expects; there is no feature-set gate in programmer. Trade-off: identical
frames across targets keep CPU/Metal/CUDA tests comparable until a backend
adds a real Metal-only pass.
*/
type MetalCompiler struct {
	tokens []Token
	frames []Frame
}

/*
NewMetalCompiler wraps tokens for a Metal-target Compile.
*/
func NewMetalCompiler(tokens []Token) *MetalCompiler {
	return &MetalCompiler{tokens: tokens}
}

/*
Compile lowers tokens to Frames with target=Metal. Payloads match CPU/CUDA
unless packTruth gains a Metal branch later.
*/
func (compiler *MetalCompiler) Compile() ([]Frame, error) {
	return newFrameBuilder(compiler.tokens).frames(Metal)
}
