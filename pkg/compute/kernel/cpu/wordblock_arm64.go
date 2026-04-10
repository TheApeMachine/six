//go:build arm64

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

	if len(frame) >= 2 {
		return hammingMatch(&frame[0], len(frame), target, maxDist)
	}

	return uint64(bits.OnesCount64(frame[0]^target)) <= maxDist
}

//go:noescape
func hammingMatch(frame *uint64, n int, target uint64, maxDist uint64) bool

//go:noescape
func universalBitwiseV2(value *uint64, numRotations int)

//go:noescape
func batchAffinityDistances(query *uint64, candidates *uint64, count int, out *uint32)

//go:noescape
func geometricFrame(value *uint64, opcode uint64) bool
