package programmer

import "github.com/theapemachine/six/pkg/primitive"

type Executable struct {
	value    *primitive.Value
	firmware string
	assets   []*Asset
	tokens   []Token
}

func NewExecutable(
	value *primitive.Value,
	firmware string,
	assets []*Asset,
) *Executable {
	return &Executable{
		value:    value,
		firmware: firmware,
		assets:   assets,
		tokens:   NewParser(NewProgram(firmware)).Parse(),
	}
}

func (executable *Executable) Compile(target CompilerTarget) ([]Frame, error) {
	return NewCompiler(executable.tokens).Compile(target)
}
