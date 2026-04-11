package programmer

import (
	"strings"

	"github.com/theapemachine/six/pkg/core"
)

/*
Program wraps around a high-level source code and prepares
it for parsing it into a tokenized representation.
*/
type Program struct {
	source     string
	lineFields [][]string
}

/*
NewProgram builds a Program from nameOrSource.

If core.Cfg.Programs has a non-empty value for that exact key, that text
is the program body. Otherwise nameOrSource is used as the full source
inline. Callers can therefore name programs in config.yml or pass raw
source when no entry exists.
*/
func NewProgram(nameOrSource string) *Program {
	resolved := nameOrSource

	if core.Cfg != nil {
		if body, ok := core.Cfg.Programs[nameOrSource]; ok && body != "" {
			resolved = body
		}
	}

	return &Program{source: resolved}
}

/*
Load materializes the program text into rows of trimmed fields.

Each outer element is one non-empty logical line. Program source uses five
columns per line (see cmd/cfg/config.yml programs:):

	srcA srcB dst op mode

Example: tokens[0,2] tokens[1,3] signals[0] xor accumulate

Leading and trailing whitespace on each line are dropped; runs of
whitespace between tokens use strings.Fields rules. Blank lines are
skipped so literal YAML blocks can include padding.

The first call builds a cache; later calls return the same slice. Do
not mutate the returned rows or fields unless you will discard the
Program afterward.
*/
func (program *Program) Load() [][]string {
	if program.lineFields != nil {
		return program.lineFields
	}

	if program.source == "" {
		program.lineFields = [][]string{}

		return program.lineFields
	}

	rows := make([][]string, 0)

	for line := range strings.SplitSeq(program.source, "\n") {
		fields := strings.Fields(line)

		if len(fields) == 0 {
			continue
		}

		rows = append(rows, fields)
	}

	program.lineFields = rows

	return program.lineFields
}
