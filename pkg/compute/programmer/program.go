package programmer

import "github.com/theapemachine/six/pkg/core"

type Program struct {
	source string
	parser *Parser
}

type programOption func(*Program)

func NewProgram(
	options ...programOption,
) *Program {
	program := &Program{}

	for _, option := range options {
		option(program)
	}

	return program
}

/*
Load first attempts to load the program from the config, and if
that fails, it will accept the source string as a literal program.
*/
func (program *Program) Load(source string) error {
	if program.source = core.Cfg.Programs[source]; program.source != "" {
		return nil
	}

	program.source = source
	return nil
}
