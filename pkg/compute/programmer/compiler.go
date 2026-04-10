package programmer

import "fmt"

type CompilerTarget uint8

const (
	CPU CompilerTarget = iota
	Metal
	CUDA
)

/*
Compiler optimizes a tokenized program into one or more optimized frames.
It ensures that each frame can be executed as a linear sweep, without any
branching or looping.
It also takes care of setting a finalizer, which is used to do things like
emit new derived Values, re-scheduling the program onto the priority queue,
for branching or looping behavior, etc.
The final executable format is a slice of contiguous frames, so there are no
limits on the size of the program.
*/
type Compiler struct {
	tokens []Token
}

type compilerOption func(*Compiler)

func NewCompiler(
	tokens []Token, options ...compilerOption,
) *Compiler {
	compiler := &Compiler{tokens: tokens}

	for _, option := range options {
		option(compiler)
	}

	return compiler
}

/*
Compile the tokens into a slice of contiguous frames, optimized for the given target.
*/
func (compiler *Compiler) Compile(target CompilerTarget) ([]Frame, error) {
	switch target {
	case CPU:
		return NewCPUCompiler(compiler.tokens).Compile()
	case Metal:
		return NewMetalCompiler(compiler.tokens).Compile()
	case CUDA:
		return NewCUDACompiler(compiler.tokens).Compile()
	}

	return nil, fmt.Errorf("unsupported target: %d", target)
}
