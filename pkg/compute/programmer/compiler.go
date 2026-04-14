package programmer

type CompilerTarget uint8

const (
	CPU CompilerTarget = iota
	Metal
	CUDA
)

/*
Compiler lowers tokens into Frames for a chosen backend target. Targets share
the same lowered words until a backend adds a specialized pass.
*/
type Compiler struct {
	builder *Builder
}

type compilerOption func(*Compiler)

/*
NewCompiler builds a compiler from an already-parsed token slice. Optional
continuation comes from Parser.Parse when the source ends with next …
*/
func NewCompiler(tokens []Token, opts ...compilerOption) *Compiler {
	compiler := &Compiler{builder: NewBuilder(tokens)}

	for _, opt := range opts {
		opt(compiler)
	}

	return compiler
}

/*
Compile emits one Frame per token, optimized for the given target.
*/
func (compiler *Compiler) Compile(target CompilerTarget) ([]Frame, error) {
	return compiler.builder.build(target)
}
