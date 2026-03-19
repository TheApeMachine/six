package primitive

/*
Opcode retrieves the low 8-bit program opcode embedded in the Guard Band.
*/
func (value Value) Opcode() uint64 {
	return value.C5() & 0xFF
}

/*
SetResidualCarry stores fractional phase state across distributed wavefront computations (Word 6).
*/
func (value Value) SetResidualCarry(carry uint64) {
	value.SetC6(carry)
}

/*
ResidualCarry retrieves fractional phase context stored in the Guard Band.
*/
func (value Value) ResidualCarry() uint64 {
	return value.C6()
}
