package primitive

/*
OperatorFlags exposes the shell-level execution flags packed into the upper 4
bits of the shell word block.
*/
func (value Value) OperatorFlags() uint16 {
	return uint16((value.Block(shellWordBlock) & shellWordMaskFlags) >> shellWordShiftFlags)
}

/*
setOperatorFlags replaces the packed flag field in the shell word block while
preserving all non-flag metadata.
*/
func (value Value) setOperatorFlags(flags uint16) {
	word := value.Block(shellWordBlock)
	word &^= shellWordMaskFlags
	word |= uint64(flags&0xF) << shellWordShiftFlags
	value.setBlock(shellWordBlock, word)
}

/*
setOperatorFlag toggles one operator flag bit by reading the current flag field,
updating the requested bit, and writing the packed flag word back.
*/
func (value Value) setOperatorFlag(flag uint16, enabled bool) {
	flags := value.OperatorFlags()

	if enabled {
		flags |= flag
	} else {
		flags &^= flag
	}

	value.setOperatorFlags(flags)
}

/*
HasOperatorFlag reports whether the requested shell-level execution flag is set.
*/
func (value Value) HasOperatorFlag(flag uint16) bool {
	return value.OperatorFlags()&flag != 0
}

/*
SetMutable marks the value as logically mutable.
*/
func (value *Value) SetMutable(mutable bool) {
	value.setOperatorFlag(ValueFlagMutable, mutable)
}

/*
Mutable reports whether the value has been marked as logically mutable.
*/
func (value *Value) Mutable() bool {
	return value.HasOperatorFlag(ValueFlagMutable)
}
