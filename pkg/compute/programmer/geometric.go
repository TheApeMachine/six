package programmer

import (
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
GeometricIntent carries the in-band operands for the PGA ALU. Rotor maps to
the Context region, Target maps to Gradient, and the backend writes the result
into Signals.
*/
type GeometricIntent struct {
	Rotor  [primitive.RegionWords]uint64
	Target [primitive.RegionWords]uint64
}

/*
NewGeometricIntent packs primitive multivectors into the uint64 lanes that the
Value frame carries to the compute backend.
*/
func NewGeometricIntent(
	rotor primitive.FrameMultivector,
	target primitive.FrameMultivector,
) *GeometricIntent {
	return &GeometricIntent{
		Rotor:  rotor.Words(),
		Target: target.Words(),
	}
}

/*
CompilerWithGeometricIntent attaches PGA operands to a Compiler without
reusing the Boolean asset layout.
*/
func CompilerWithGeometricIntent(geometric *GeometricIntent) compilerOption {
	return func(compiler *Compiler) {
		compiler.intent.Geometric = geometric
	}
}

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

	if intent.Geometric != nil {
		compiler.writeRegionWordArray(value, kernel.ContextStartWord, intent.Geometric.Rotor)
		compiler.writeRegionWordArray(value, kernel.GradientStartWord, intent.Geometric.Target)

		return true
	}

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

func (compiler *Compiler) writeRegionWordArray(
	value *primitive.Value,
	start int,
	words [primitive.RegionWords]uint64,
) {
	for wordIdx := range primitive.RegionWords {
		value.Set(start+wordIdx, words[wordIdx])
	}
}
