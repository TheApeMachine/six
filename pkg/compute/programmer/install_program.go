package programmer

import (
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
InstallProgram lowers nameOrSource into the Value program region using the
same surface as everywhere else: NewProgram for config vs inline resolution,
then Parse → Compile(CPU) → the first Frame’s writeIntoProgramRegion.

A single substrate pass consumes one Frame; multi-line programs belong on
Executable.Run.
*/
type Installer struct{}

func (i Installer) InstallProgram(value *primitive.Value, nameOrSource string) error {
	if value == nil {
		return nil
	}

	cp, ok := getCachedProgram(nameOrSource)
	if ok {
		return i.installCached(value, cp.frames, cp.cont)
	}

	program := NewProgram(nameOrSource)
	load := program.Load()

	if len(load) == 0 {
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

	setCachedProgram(nameOrSource, cachedProgram{frames: frames, cont: cont})

	return i.installCached(value, frames, cont)
}

func (i Installer) installCached(value *primitive.Value, frames []Frame, cont *Continuation) error {
	if len(frames) == 0 {
		return nil
	}

	if len(frames) > 1 {
		// Install multi-line program into the Reserved region
		for idx, frame := range frames {
			if idx*6 >= 60 { // Max 10 instructions in 64-word Reserved region
				break
			}
			for j := 0; j < 6; j++ {
				(*value)[kernel.ReservedStartWord+idx*6+j] = frame.Program[j]
			}
		}
		// Set OpcodeRegionProgram to trigger execution from Reserved region
		(*value)[kernel.ProgramOpcodeWord] = kernel.OpcodeRegionProgram
	} else {
		frames[0].writeIntoProgramRegion(value)
	}

	// Apply continuation if present
	if cont != nil {
		cont.ApplyScheduling(value)
	}

	return nil
}
