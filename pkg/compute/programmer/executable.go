package programmer

import "github.com/theapemachine/six/pkg/primitive"

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
	value     *primitive.Value
	firmware  string
	assets    []*Asset
	tokens    []Token
	err       error
	finalizer Finalizer
}

func NewExecutable(
	value *primitive.Value,
	firmware string,
	assets []*Asset,
) *Executable {
	for _, asset := range assets {
		_ = asset.Bundle(value)
	}

	parser := NewParser(NewProgram(firmware))
	tokens := parser.Parse()

	return &Executable{
		value:    value,
		firmware: firmware,
		assets:   assets,
		tokens:   tokens,
		err:      parser.Err(),
	}
}

/*
Value returns the underlying Value so the Backend can obtain the frame
pointer and write compiled program words.
*/
func (executable *Executable) Value() *primitive.Value {
	return executable.value
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

	return NewCompiler(executable.tokens).Compile(target)
}
