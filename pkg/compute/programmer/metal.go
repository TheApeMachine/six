package programmer

/*
MetalCompiler is an optimizer for Metal GPU execution.
*/
type MetalCompiler struct {
	tokens []Token
	frames []Frame
}

/*
NewMetalCompiler creates a new MetalCompiler.
*/
func NewMetalCompiler(tokens []Token) *MetalCompiler {
	return &MetalCompiler{tokens: tokens}
}

/*
Compile the program and optimize for Metal GPU execution.
*/
func (compiler *MetalCompiler) Compile() ([]Frame, error) {
	return newFrameBuilder(compiler.tokens).frames(Metal)
}
