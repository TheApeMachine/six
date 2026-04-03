//go:build !amd64 && !arm64

package cpu

import (
	"math/bits"
)

// execWordBlock dispatches to the scalar Go kernel. On arm64 (Apple Silicon,
// etc.) the compiler emits NEON for the simple inner loops automatically.
func execWordBlock(dst, src []uint64, op uint8) {
	execWordBlockScalar(dst, src, op)
}

// HasHammingMatch returns true if any word in frame has
// popcount(word ^ target) <= maxDist.
func HasHammingMatch(frame []uint64, target uint64, maxDist uint64) bool {
	for _, w := range frame {
		if uint64(bits.OnesCount64(w^target)) <= maxDist {
			return true
		}
	}
	return false
}
