package cpu

/*
ExecWordBlock applies the same word-wise opcode as UniversalBitwise ALU across
parallel lanes. dst and src must have equal length; results are written to dst.
This is the SIMD-backed path used for homogeneous batched execution.
*/
func ExecWordBlock(dst, src []uint64, op uint8) {
	execWordBlock(dst, src, op)
}
