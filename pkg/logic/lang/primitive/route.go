package primitive

/*
SetRouteHint stores a compact route class that biases the next hop toward
compatible continuation cells without redundantly storing a lexical byte.
*/
func (value Value) SetRouteHint(route uint8) {
	word := value.C7()
	word &^= shellWordMaskRouteHint
	word |= uint64(route) << shellWordShiftRouteHint
	value.SetC7(word)
	value.setOperatorFlag(ValueFlagRouteHint, true)
}

/*
RouteHint retrieves the shell-level continuation class carried by the value.
*/
func (value Value) RouteHint() uint8 {
	return uint8((value.C7() & shellWordMaskRouteHint) >> shellWordShiftRouteHint)
}

/*
HasRouteHint reports whether the value carries a route hint.
*/
func (value Value) HasRouteHint() bool {
	return value.HasOperatorFlag(ValueFlagRouteHint)
}
