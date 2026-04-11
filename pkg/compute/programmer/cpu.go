package programmer

/*
CPUCompiler is an optimizer for CPU execution.
*/
type CPUCompiler struct {
	tokens []Token
	frames []Frame
}

/*
NewCPUCompiler creates a new CPUCompiler.
*/
func NewCPUCompiler(tokens []Token) *CPUCompiler {
	return &CPUCompiler{tokens: tokens}
}

/*
Compile the program and optimize for CPU execution.
*/
func (compiler *CPUCompiler) Compile() ([]Frame, error) {
	return newFrameBuilder(compiler.tokens).frames(CPU)
}
