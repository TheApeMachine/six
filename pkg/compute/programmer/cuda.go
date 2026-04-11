package programmer

/*
CUDACompiler is an optimizer for CUDA GPU execution.
*/
type CUDACompiler struct {
	tokens []Token
	frames []Frame
}

/*
NewCUDACompiler creates a new CUDACompiler.
*/
func NewCUDACompiler(tokens []Token) *CUDACompiler {
	return &CUDACompiler{tokens: tokens}
}

/*
Compile the program and optimize for CUDA GPU execution.
*/
func (compiler *CUDACompiler) Compile() ([]Frame, error) {
	return newFrameBuilder(compiler.tokens).frames(CUDA)
}
