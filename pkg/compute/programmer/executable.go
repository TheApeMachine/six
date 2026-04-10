package programmer

import "github.com/theapemachine/six/pkg/primitive"

/*
Executable is a compiled program that is ready to be executed.
*/
type Executable struct {
	compiler  *Compiler
	finalizer func(*primitive.Value) ([]*primitive.Value, error)
}

func NewExecutable(
	compiler *Compiler,
	finalizer func(*primitive.Value) ([]*primitive.Value, error),
) *Executable {
	return &Executable{compiler: compiler, finalizer: finalizer}
}

/*
Execute runs the executable.
*/
func (executable *Executable) Execute(target CompilerTarget) (frames []Frame, err error) {
	if frames, err = executable.compiler.Compile(target); err != nil {
		return nil, err
	}

	return frames, nil
}

/*
Finalize runs the finalizer on the output frames.
*/
func (executable *Executable) Finalize(frames *primitive.Value) ([]*primitive.Value, error) {
	return executable.finalizer(frames)
}
