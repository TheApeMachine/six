package primitive

/*
OperatorFlags exposes the shell-level execution flags packed into the upper 12
bits of the affine word.
*/
func (value Value) OperatorFlags() uint16 {
	return uint16((value.C7() & shellWordMaskFlags) >> shellWordShiftFlags)
}

/*
setOperatorFlags replaces the packed 12-bit operator flag field in C7 while
preserving all non-flag shell metadata in the same word.
*/
func (value Value) setOperatorFlags(flags uint16) {
	word := value.C7()
	word &^= shellWordMaskFlags
	word |= uint64(flags&0x0FFF) << shellWordShiftFlags
	value.SetC7(word)
}

/*
setOperatorFlag toggles one operator flag bit by reading the current flag field,
updating the requested bit, and writing the packed flag word back to C7.
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
SetMutable marks the value as logically mutable. This does not mutate storage in
place; it merely records that the operator may be versioned append-only in the
LSM when updated.
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
