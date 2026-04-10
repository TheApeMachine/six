package programmer

import (
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Executor is the interface that the compute backend satisfies. It
allows the Queue to dispatch compiled programs without importing
the compute package (which already imports pool).
*/
type Executor interface {
	CompileAndExecute(program *Compiler) error
}

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
	value                  *primitive.Value
	intent                 Intent
	useBatchAffinityLayout bool
	finalizers             []Finalizer
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
		compiler.CPU(compiler.value, compiler.intent, compiler.useBatchAffinityLayout)
	case Metal:
		compiler.Metal(compiler.value, compiler.intent, compiler.useBatchAffinityLayout)
	case CUDA:
		compiler.CUDA(compiler.value, compiler.intent, compiler.useBatchAffinityLayout)
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

/*
Intent returns the current compile intent (operation and assets).
*/
func (compiler *Compiler) Intent() Intent {
	if compiler == nil {
		return Intent{}
	}

	return compiler.intent
}

/*
FinalizerDepth is the number of post-execute finalizers chained on Finalize.
*/
func (compiler *Compiler) FinalizerDepth() int {
	if compiler == nil {
		return 0
	}

	return len(compiler.finalizers)
}

/*
UsesBatchAffinityLayout reports whether compile used the batch nearest-affinity layout.
*/
func (compiler *Compiler) UsesBatchAffinityLayout() bool {
	return compiler != nil && compiler.useBatchAffinityLayout
}

func CompilerWithIntent(intent Intent) compilerOption {
	return func(compiler *Compiler) {
		compiler.intent = intent
	}
}

/*
CompilerWithFinalizer appends a post-execution finalizer to the compiler.
Finalizers run only when Finalize is called by the owner after execution
has completed, for example after queue.ExecuteSync returns.
*/
func CompilerWithFinalizer(finalizer Finalizer) compilerOption {
	return func(compiler *Compiler) {
		if finalizer == nil {
			return
		}

		compiler.finalizers = append(compiler.finalizers, finalizer)
	}
}

/*
CompilerWithBatchAffinityLayout selects the contiguous 8-word-per-candidate
layout at word 32 for opcode 0x6 batch nearest-affinity kernels. Without
this flag, opcode 0x6 still uses the rotation arena for truth-table work
(e.g. Bind). Only callers that reduce argmin in the batch kernel should set this.
*/
func CompilerWithBatchAffinityLayout() compilerOption {
	return func(compiler *Compiler) {
		compiler.useBatchAffinityLayout = true
	}
}

/*
Finalize runs the compiler's post-execution finalizer chain against the frame.
The compiler does not call this automatically because asynchronous queue
execution has nowhere to return emitted Values; the caller owns that decision.
*/
func (compiler *Compiler) Finalize() ([]*primitive.Value, error) {
	if compiler == nil || compiler.value == nil {
		return nil, nil
	}

	finalize := FinalizeNext(func(*primitive.Value) ([]*primitive.Value, error) {
		return nil, nil
	})

	for idx := len(compiler.finalizers) - 1; idx >= 0; idx-- {
		current := compiler.finalizers[idx]
		next := finalize

		finalize = FinalizeNext(func(value *primitive.Value) ([]*primitive.Value, error) {
			if current == nil {
				return next(value)
			}

			return current(value, next)
		})
	}

	return finalize(compiler.value)
}
