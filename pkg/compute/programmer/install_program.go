package programmer

import "github.com/theapemachine/six/pkg/primitive"

/*
InstallProgram lowers nameOrSource into the Value program region using the
same surface as everywhere else: NewProgram for config vs inline resolution,
then Parse → Compile(CPU) → the first Frame’s writeIntoProgramRegion.

A single substrate pass consumes one Frame; multi-line programs belong on
Executable.Run.
*/
func InstallProgram(value *primitive.Value, nameOrSource string) error {
	if value == nil {
		return nil
	}

	program := NewProgram(nameOrSource)

	if len(program.Load()) == 0 {
		return nil
	}

	tokens, cont, err := NewParser(program).Parse()

	if err != nil {
		return err
	}

	compiler := NewCompiler(tokens, WithContinuation(cont))

	frames, err := compiler.Compile(CPU)

	if err != nil {
		return err
	}

	if len(frames) == 0 {
		return nil
	}

	frames[0].writeIntoProgramRegion(value)

	return nil
}
