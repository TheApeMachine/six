package programmer

import (
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Finalizer is a post-execution callback attached to an Executable. After the
substrate finishes, the Backend calls Finalize which runs this function. The
Finalizer reads signals, emits new Values, or re-submits the Value for the
next firmware pass — it is the only way to observe results in fire-and-forget
execution.
*/
type Finalizer func(value *primitive.Value)

/*
Executable pairs a Value with its parsed firmware tokens and an optional
post-execution Finalizer. The Backend calls Compile with the chosen
CompilerTarget, writes the resulting Frames into the Value, executes on the
substrate, and then calls Finalize.
*/
type Executable struct {
	value        *primitive.Value
	firmware     string
	tokens       []Token
	continuation *Continuation
	resident     bool
	err          error
	finalizer    Finalizer
}

func NewExecutable(
	value *primitive.Value,
	firmware string,
) *Executable {
	parser := NewParser(NewProgram(firmware))
	tokens := parser.Parse()

	return &Executable{
		value:        value,
		firmware:     firmware,
		tokens:       tokens,
		continuation: parser.Continuation(),
		err:          parser.Err(),
	}
}

func NewResidentExecutable(value *primitive.Value) *Executable {
	return &Executable{
		value:    value,
		resident: true,
	}
}

/*
Value returns the underlying Value so the Backend can obtain the frame
pointer and write compiled program words.
*/
func (executable *Executable) Value() *primitive.Value {
	return executable.value
}

func (executable *Executable) IsResidentProgram() bool {
	if executable == nil {
		return false
	}

	return executable.resident
}

/*
Firmware returns the configured firmware name this Executable will run
(empty when resident). Useful for observers and tests that need to
verify which rule fired for a given Value without pulling open the
parsed-token soup.
*/
func (executable *Executable) Firmware() string {
	if executable == nil {
		return ""
	}

	return executable.firmware
}

/*
SetFinalizer attaches a post-execution callback.
*/
func (executable *Executable) SetFinalizer(finalizer Finalizer) {
	executable.finalizer = finalizer
}

/*
Finalize runs the post-execution callback if one was set.
*/
func (executable *Executable) Finalize() {
	if executable.finalizer != nil {
		executable.finalizer(executable.value)
	}
}

func (executable *Executable) Compile(target CompilerTarget) ([]Frame, error) {
	if executable.err != nil {
		return nil, executable.err
	}

	if executable.resident {
		return nil, nil
	}

	return NewCompiler(executable.tokens).Compile(target)
}

func (executable *Executable) ApplyContinuation() {
	if executable == nil || executable.value == nil {
		return
	}

	if executable.continuation == nil {
		executable.value.Set(kernel.SchedulingNextProgramWord, 0)
		return
	}

	if executable.continuation.Self {
		executable.value.Set(kernel.SchedulingNextProgramWord, executable.value.ID())
		return
	}

	executable.value.Set(kernel.SchedulingNextProgramWord, executable.continuation.ValueID)
}
