package primitive

import config "github.com/theapemachine/six/pkg/system/core"

const (
	opcodeBlock   = config.CoreBlocks
	residualBlock = config.CoreBlocks + 1
)

/*
Opcode retrieves the low 8-bit program opcode embedded in the shell.
*/
func (value Value) Opcode() uint64 {
	return value.Block(opcodeBlock) & 0xFF
}

/*
SetResidualCarry stores fractional phase state across distributed wavefront
computations in the residual shell block.
*/
func (value Value) SetResidualCarry(carry uint64) {
	value.setBlock(residualBlock, carry)
}

/*
ResidualCarry retrieves fractional phase context stored in the shell.
*/
func (value Value) ResidualCarry() uint64 {
	return value.Block(residualBlock)
}
