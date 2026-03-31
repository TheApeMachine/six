//go:build !amd64

package cpu

// execWordBlock dispatches to the scalar Go kernel. On arm64 (Apple Silicon,
// etc.) the compiler emits NEON for the simple inner loops automatically.
func execWordBlock(dst, src []uint64, op uint8) {
	execWordBlockScalar(dst, src, op)
}
