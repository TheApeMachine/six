//go:build !amd64 && !arm64

package cpu

import "math/bits"

// HammingMatch returns true if any word in frame has
// popcount(word ^ target) <= maxDist.
func (backend *Backend) HammingMatch(
	frame []uint64, target uint64, maxDist uint64,
) bool {
	for _, w := range frame {
		if uint64(bits.OnesCount64(w^target)) <= maxDist {
			return true
		}
	}

	return false
}
