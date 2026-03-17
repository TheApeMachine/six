package data

/*
Sanitize enforces the lower 257-bit field width for fundamental field comparisons,
but preserves the upper 255 bits (Guard Band) which is now utilized for
Cross-Modal Alignment, Rotational Opcodes, and Residual Phase Carry.
*/
func (value *Value) Sanitize() {
	value.SetC4(value.C4() & 1) // Bit 256 is the delimiter
	// Words 5, 6, and 7 are deliberately kept alive as the Guard Band for Opcodes
	// See SetOpcode and SetResidualCarry.
}

/*
SetOpcode stores the low 8-bit program opcode in the Guard Band while preserving
all other control-plane fields packed into word 5.
*/
func (value *Value) SetOpcode(opcode uint64) {
	word := value.C5()
	word &^= 0xFF
	word |= opcode & 0xFF
	value.SetC5(word)
}

/*
Opcode retrieves the low 8-bit program opcode embedded in the Guard Band.
*/
func (value *Value) Opcode() uint64 {
	return value.C5() & 0xFF
}

/*
SetResidualCarry stores fractional phase state across distributed wavefront computations (Word 6).
*/
func (value *Value) SetResidualCarry(carry uint64) {
	value.SetC6(carry)
}

/*
ResidualCarry retrieves fractional phase context stored in the Guard Band.
*/
func (value *Value) ResidualCarry() uint64 {
	return value.C6()
}
