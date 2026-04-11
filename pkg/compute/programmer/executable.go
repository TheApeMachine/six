package programmer

import (
	"fmt"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Executable binds compilation to execution: a Compiler plus optional ingress
Values and an optional finalizer that can emit follow-on Values.

Inputs carry the starting Value wire into the run: every emitted Value
starts as a copy of inputs[0] when present, then the compiled program
region is stamped on top. The substrate reads srcA / srcB and writes dst
directly out of the Value — Executable no longer stages or writes back
operand bands itself, because packTruth encoded the absolute region
starts and spans into the program words universalBitwiseV2 decodes at
dispatch time.
*/
type Executable struct {
	compiler  *Compiler
	inputs    []*primitive.Value
	finalizer func(*primitive.Value) ([]*primitive.Value, error)
}

func NewExecutable(
	compiler *Compiler,
	finalizer func(*primitive.Value) ([]*primitive.Value, error),
) *Executable {
	return &Executable{compiler: compiler, finalizer: finalizer}
}

/*
Inputs returns the ingress slice (may be nil). Callers own the Values.
*/
func (executable *Executable) Inputs() []*primitive.Value {
	return executable.inputs
}

/*
Execute compiles the program and materializes one unsafe.Pointer per
emitted frame. Each pointer is the base of a full primitive.Value minted
from inputs[0] when present (otherwise a zero wire), with the compiled
program region stamped on top. The caller is expected to hand the pointer
slice to a substrate.

Scheduling from the optional continuation is written on the last emitted
Value (word 117).
*/
func (executable *Executable) Execute(target CompilerTarget) ([]unsafe.Pointer, error) {
	frames, err := executable.compiler.Compile(target)

	if err != nil {
		return nil, err
	}

	tokens := executable.compiler.Tokens()

	if len(tokens) != len(frames) {
		return nil, fmt.Errorf("programmer: token/frame count mismatch: %d tokens, %d frames",
			len(tokens), len(frames))
	}

	out := make([]unsafe.Pointer, 0, len(frames))
	cont := executable.compiler.Continuation()

	for idx := range frames {
		value := executable.valueForFrame()

		frames[idx].writeIntoProgramRegion(value)

		if idx == len(frames)-1 && cont != nil {
			cont.ApplyScheduling(value)
		}

		out = append(out, unsafe.Pointer(&(*value)[0]))
	}

	return out, nil
}

/*
Run drives one Value through a full program: every frame stamps its
program region onto the same Value and the substrate runs a pass that
reads srcA / srcB and writes dst in place. Frames chain because they all
mutate the same Value, which is what lets an accumulate chain build up
across lines.

The caller supplies a kernel.Substrate; this keeps programmer free of a
compile-time cpu/metal/cuda dependency and matches the interface every
backend already implements.
*/
func (executable *Executable) Run(
	target CompilerTarget,
	substrate kernel.Substrate,
) (*primitive.Value, error) {
	if substrate == nil {
		return nil, fmt.Errorf("programmer: Run requires a substrate")
	}

	frames, err := executable.compiler.Compile(target)

	if err != nil {
		return nil, err
	}

	tokens := executable.compiler.Tokens()

	if len(tokens) != len(frames) {
		return nil, fmt.Errorf("programmer: token/frame count mismatch: %d tokens, %d frames",
			len(tokens), len(frames))
	}

	value := executable.valueForFrame()
	cont := executable.compiler.Continuation()

	ptr := []unsafe.Pointer{unsafe.Pointer(&(*value)[0])}

	for idx := range frames {
		frames[idx].writeIntoProgramRegion(value)

		if err := substrate.Execute(ptr); err != nil {
			return nil, err
		}
	}

	if cont != nil {
		cont.ApplyScheduling(value)
	}

	return value, nil
}

/*
Finalize runs the finalizer on one post-execution Value. When finalizer is
nil, returns a single-element slice containing that Value.
*/
func (executable *Executable) Finalize(out *primitive.Value) ([]*primitive.Value, error) {
	if executable.finalizer == nil {
		return []*primitive.Value{out}, nil
	}

	return executable.finalizer(out)
}

/*
WithInputs attaches Values whose wire frames seed emitted Values before the
compiled program is written. Returns the same Executable for chaining.
*/
func (executable *Executable) WithInputs(inputs []*primitive.Value) *Executable {
	executable.inputs = inputs

	return executable
}

/*
valueForFrame mints a Value wire for one frame: a copy of inputs[0] when
set, otherwise a zero wire.
*/
func (executable *Executable) valueForFrame() *primitive.Value {
	if len(executable.inputs) > 0 && executable.inputs[0] != nil {
		v := *executable.inputs[0]

		return &v
	}

	out := primitive.Value{}

	return &out
}
