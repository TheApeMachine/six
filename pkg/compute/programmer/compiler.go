package programmer

import (
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Target identifies which compute substrate the Value
will execute on. The compiler emits a different
layout for each target.
*/
type Target uint

const (
	CPU Target = iota
	Metal
	CUDA
)

/*
Intent describes what the Value needs to compute.
The frontend methods on Programmer build an Intent,
and the backend compiles it into a substrate-specific
layout inside the Value.
*/
type Intent struct {
	Operation Operation
	Assets    [][]uint64
}

/*
Operation is what the Value needs to do with its
data and assets. Each operation maps to a truth
table opcode, but the compiler decides how to
arrange data around it.
*/
type Operation uint64

const (
	Similarity Operation = 0x1 // AND — how much overlap
	Distance   Operation = 0x6 // XOR + popcount — how different
	Bind       Operation = 0x6 // XOR — associative pairing
	Bundle     Operation = 0x7 // OR — superposition
)

/*
Compiler for Values. It takes
high-level intent — "compute distance against
these assets" — and compiles it into a layout
that the target substrate can blast through
without branching or decisions.

The compilation has three stages:

 1. Frontend — describe intent via method calls
 2. Optimizer — figure out passes, packing order,
    rotation strategy
 3. Backend — emit a target-specific layout into
    the Value's regions
*/
type Compiler struct {
	value  *primitive.Value
	intent Intent
}

type compilerOption func(*Compiler)

/*
New creates a Compiler for the given Value.
The Value's token region (words 0-3) is treated
as the query — A in the ALU.
*/
func New(value *primitive.Value, options ...compilerOption) *Compiler {
	compiler := &Compiler{value: value}

	for _, option := range options {
		option(compiler)
	}

	return compiler
}

/*
Compile runs the optimizer and emits the layout
for the given target substrate. After this call,
the Value is ready to be submitted to the ALU.
*/
func (compiler *Compiler) Compile(
	target Target,
) *primitive.Value {
	switch target {
	case CPU:
		compiler.CPU(compiler.value, compiler.intent)
	case Metal:
		compiler.Metal(compiler.value, compiler.intent)
	case CUDA:
		compiler.CUDA(compiler.value, compiler.intent)
	}

	return compiler.value
}

/*
Frame returns the Value the compiler mutates so callers can attach
correlation and observe residency hints before CompileAndExecute.
*/
func (compiler *Compiler) Frame() *primitive.Value {
	return compiler.value
}

func CompilerWithIntent(intent Intent) compilerOption {
	return func(compiler *Compiler) {
		compiler.intent = intent
	}
}
