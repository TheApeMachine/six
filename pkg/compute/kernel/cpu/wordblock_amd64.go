//go:build amd64

package cpu

import "math/bits"

/*
HammingMatch returns true if any word in frame has
popcount(word ^ target) <= maxDist.
*/
func (backend *Backend) HammingMatch(
	frame []uint64, target uint64, maxDist uint64,
) bool {
	if len(frame) == 0 {
		return false
	}

	for _, w := range frame {
		if uint64(bits.OnesCount64(w^target)) <= maxDist {
			return true
		}
	}

	return false
}

//go:noescape
func popcount(dst, src *uint64, n int)

//go:noescape
func hammingMatch(frame *uint64, n int, target uint64, maxDist uint64) bool

//go:noescape
func universalBitwise(dst *uint64, a, b, m0, m1, m2, m3 *uint64)

//go:noescape
func universalBitwiseV2(value *uint64, numRotations int)

//go:noescape
func batchAffinityDistances(query *uint64, candidates *uint64, count int, out *uint32)
