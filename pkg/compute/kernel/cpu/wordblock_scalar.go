package cpu

// execWordBlockScalar applies a 4-bit truth-table opcode across aligned
// slices of uint64 words. The opcode IS the truth table.
func execWordBlockScalar(dst, src []uint64, op uint8) {
	n := min(len(src), len(dst))
	dst = dst[:n]
	src = src[:n]

	for i := range dst {
		dst[i] = ExecWord(op, src[i], dst[i])
	}
}
