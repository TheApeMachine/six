package primitive

const (
	routeHintShift = 8
	routeHintMask  = uint64(0xFF) << routeHintShift
)

/*
SetRouteHint stores a compact route class in the opcode shell block (bits 8-15)
that biases the next hop toward compatible continuation cells.
*/
func (value Value) SetRouteHint(route uint8) {
	word := value.Block(opcodeBlock)
	word &^= routeHintMask
	word |= uint64(route) << routeHintShift
	value.setBlock(opcodeBlock, word)
	value.setOperatorFlag(ValueFlagRouteHint, true)
}

/*
RouteHint retrieves the shell-level continuation class carried by the value.
*/
func (value Value) RouteHint() uint8 {
	return uint8((value.Block(opcodeBlock) & routeHintMask) >> routeHintShift)
}

/*
HasRouteHint reports whether the value carries a route hint.
*/
func (value Value) HasRouteHint() bool {
	return value.HasOperatorFlag(ValueFlagRouteHint)
}
