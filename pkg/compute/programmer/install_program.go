package programmer

import (
	"fmt"

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
		const (
			wordsPerRegionFrame    = 6
			reservedProgramWords   = 60
			maxRegionProgramFrames = reservedProgramWords / wordsPerRegionFrame
		)

		if len(frames) > maxRegionProgramFrames {
			return fmt.Errorf(
				"programmer: install program has %d frames; reserved region program table holds at most %d frames (%d words, %d words per entry)",
				len(frames),
				maxRegionProgramFrames,
				reservedProgramWords,
				wordsPerRegionFrame,
			)
		}

		// Install multi-line program into the Reserved region
		for idx, frame := range frames {
			for wordOffset := 0; wordOffset < wordsPerRegionFrame; wordOffset++ {
				(*value)[kernel.ReservedStartWord+idx*wordsPerRegionFrame+wordOffset] = frame.Program[wordOffset]
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
