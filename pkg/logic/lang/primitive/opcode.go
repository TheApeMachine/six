package primitive

/*
Opcode is the control-plane instruction stored in the Guard Band.
The lower 257 bits remain the resonant state field; word 5 carries the
threaded-code metadata used for traversal.
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
	opcodeWordShiftJump     = 8
	opcodeWordShiftBranches = 40
	opcodeWordShiftTerminal = 48
	opcodeWordMaskOpcode    = uint64(0xFF)
	opcodeWordMaskJump      = uint64(0xFFFFFFFF) << opcodeWordShiftJump
	opcodeWordMaskBranches  = uint64(0xFF) << opcodeWordShiftBranches
	opcodeWordMaskTerminal  = uint64(1) << opcodeWordShiftTerminal
)

/*
SetJump stores the relative program jump in word 5 while preserving the opcode
and other control-plane flags.
*/
func (value Value) SetJump(jump uint32) {
	word := value.C5()
	word &^= opcodeWordMaskJump
	word |= uint64(jump) << opcodeWordShiftJump
	value.SetC5(word)
}

/*
Jump retrieves the relative program jump stored in word 5.
*/
func (value Value) Jump() uint32 {
	return uint32((value.C5() & opcodeWordMaskJump) >> opcodeWordShiftJump)
}

/*
SetBranches stores the branch fan-out count in word 5.
*/
func (value Value) SetBranches(branches uint8) {
	word := value.C5()
	word &^= opcodeWordMaskBranches
	word |= uint64(branches) << opcodeWordShiftBranches
	value.SetC5(word)
}

/*
Branches retrieves the branch fan-out count from word 5.
*/
func (value Value) Branches() uint8 {
	return uint8((value.C5() & opcodeWordMaskBranches) >> opcodeWordShiftBranches)
}

/*
SetTerminal marks or clears the terminal flag in word 5.
*/
func (value Value) SetTerminal(terminal bool) {
	word := value.C5()
	word &^= opcodeWordMaskTerminal
	if terminal {
		word |= opcodeWordMaskTerminal
	}
	value.SetC5(word)
}

/*
Terminal reports whether the value marks a terminal program state.
*/
func (value Value) Terminal() bool {
	return value.C5()&opcodeWordMaskTerminal != 0
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
	value.SetC5(word)
}

/*
Program returns the threaded-code instruction stored in word 5.
*/
func (value Value) Program() (Opcode, uint32, uint8, bool) {
	return Opcode(value.Opcode()), value.Jump(), value.Branches(), value.Terminal()
}
