package programmer

import (
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
compileGeometricLayout writes a full-byte PGA opcode and optional 512-bit
operands into the Value frame. The geometric ALU consumes Context as operand
A or motor, Gradient as operand B or target, and writes its result to Signals.
*/
func (compiler *Compiler) compileGeometricLayout(
	value *primitive.Value,
	intent Intent,
) bool {
	opcode := uint64(intent.Operation)

	if !kernel.IsGeometricOpcode(opcode) {
		return false
	}

	value.Set(core.Cfg.Value.Region.Program.Start, opcode)

	if len(intent.Assets) > 0 {
		compiler.writeRegionWords(value, kernel.ContextStartWord, intent.Assets[0])
	}

	if len(intent.Assets) > 1 {
		compiler.writeRegionWords(value, kernel.GradientStartWord, intent.Assets[1])
	}

	return true
}

func (compiler *Compiler) writeRegionWords(value *primitive.Value, start int, words []uint64) {
	for wordIdx := 0; wordIdx < primitive.RegionWords && wordIdx < len(words); wordIdx++ {
		value.Set(start+wordIdx, words[wordIdx])
	}
}
