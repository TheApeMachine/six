package primitive

/*
Opcode is the control-plane instruction stored in the opcode shell block.
The lower 8191 bits remain the resonant state field; the opcode block carries
the threaded-code metadata used for traversal.
*/
type Opcode uint8

const (
	OpcodeNext Opcode = iota + 1
	OpcodeJump
	OpcodeBranch
	OpcodeReset
	OpcodeHalt
)

const (
	opcodeWordShiftJump     = 16
	opcodeWordShiftBranches = 48
	opcodeWordShiftTerminal = 56
	opcodeWordMaskOpcode    = uint64(0xFF)
	opcodeWordMaskJump      = uint64(0xFFFFFFFF) << opcodeWordShiftJump
	opcodeWordMaskBranches  = uint64(0xFF) << opcodeWordShiftBranches
	opcodeWordMaskTerminal  = uint64(1) << opcodeWordShiftTerminal
)

/*
SetJump stores the relative program jump in the opcode block while preserving
the opcode and other control-plane flags.
*/
func (value Value) SetJump(jump uint32) {
	word := value.Block(opcodeBlock)
	word &^= opcodeWordMaskJump
	word |= uint64(jump) << opcodeWordShiftJump
	value.setBlock(opcodeBlock, word)
}

/*
Jump retrieves the relative program jump stored in the opcode block.
*/
func (value Value) Jump() uint32 {
	return uint32((value.Block(opcodeBlock) & opcodeWordMaskJump) >> opcodeWordShiftJump)
}

/*
SetBranches stores the branch fan-out count in the opcode block.
*/
func (value Value) SetBranches(branches uint8) {
	word := value.Block(opcodeBlock)
	word &^= opcodeWordMaskBranches
	word |= uint64(branches) << opcodeWordShiftBranches
	value.setBlock(opcodeBlock, word)
}

/*
Branches retrieves the branch fan-out count from the opcode block.
*/
func (value Value) Branches() uint8 {
	return uint8((value.Block(opcodeBlock) & opcodeWordMaskBranches) >> opcodeWordShiftBranches)
}

/*
SetTerminal marks or clears the terminal flag in the opcode block.
*/
func (value Value) SetTerminal(terminal bool) {
	word := value.Block(opcodeBlock)
	word &^= opcodeWordMaskTerminal

	if terminal {
		word |= opcodeWordMaskTerminal
	}

	value.setBlock(opcodeBlock, word)
}

/*
Terminal reports whether the value marks a terminal program state.
*/
func (value Value) Terminal() bool {
	return value.Block(opcodeBlock)&opcodeWordMaskTerminal != 0
}

/*
SetProgram writes the traversal instruction, relative jump, branch count,
and terminal flag in a single operation.
*/
func (value Value) SetProgram(opcode Opcode, jump uint32, branches uint8, terminal bool) {
	word := uint64(opcode) & opcodeWordMaskOpcode
	word |= uint64(jump) << opcodeWordShiftJump
	word |= uint64(branches) << opcodeWordShiftBranches

	if terminal {
		word |= opcodeWordMaskTerminal
	}

	value.setBlock(opcodeBlock, word)
}

/*
Program returns the threaded-code instruction stored in the opcode block.
*/
func (value Value) Program() (Opcode, uint32, uint8, bool) {
	return Opcode(value.Opcode()), value.Jump(), value.Branches(), value.Terminal()
}
