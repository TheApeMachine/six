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

/*
universalBitwise runs the SIMD truth-table over a fixed 64-word A×B surface
(assembly ABI). Not the same as universalBitwiseSweep in
wordblock_universal.go, which applies program region refs on the full
128-word Value frame.
*/
//go:noescape
func universalBitwise(dst *uint64, a, b, m0, m1, m2, m3 *uint64)

//go:noescape
func batchAffinityDistances(query *uint64, candidates *uint64, count int, out *uint32)

//go:noescape
func geometricFrame(value *uint64, opcode uint64) bool

//go:noescape
func affinityPopcount(vec *uint64) uint32

//go:noescape
func affinityCoupling(a *uint64, b *uint64, out *uint32)
