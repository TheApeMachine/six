//go:build arm64

package cpu

import "math/bits"

/*
execWordBlock applies a 4-bit truth-table opcode across all lanes.
NEON assembly is used for opcodes that map directly to a single SIMD
instruction; all others use the branchless TruthTable scalar loop.
*/
func execWordBlock(dst, src []uint64, op uint8) {
	n := min(len(src), len(dst))

	if n == 0 {
		return
	}

	dst = dst[:n]
	src = src[:n]

	switch op {
	case 0x0:
		clear(dst)
		return
	case 0x1:
		simdAnd(&dst[0], &src[0], n)
		return
	case 0x2:
		simdSrcAndNotDst(&dst[0], &src[0], n)
		return
	case 0x5:
		return
	case 0x6:
		simdXor(&dst[0], &src[0], n)
		return
	case 0x7:
		simdOr(&dst[0], &src[0], n)
		return
	case 0x10:
		simdPopcnt(&dst[0], &src[0], n)
		return
	case 0x11:
		simdShl(&dst[0], &src[0], n)
		return
	case 0x12:
		simdShr(&dst[0], &src[0], n)
		return
	}

	execWordBlockScalar(dst, src, op)
}

// HasHammingMatch returns true if any word in frame has
// popcount(word ^ target) <= maxDist.
// Uses the NEON simdHasHammingMatch with early exit.
func HasHammingMatch(frame []uint64, target uint64, maxDist uint64) bool {
	if len(frame) == 0 {
		return false
	}
	if len(frame) >= 2 {
		return simdHasHammingMatch(&frame[0], len(frame), target, maxDist)
	}
	// Single-word fallback (simdHasHammingMatch requires n >= 2 for its main loop).
	return uint64(bits.OnesCount64(frame[0]^target)) <= maxDist
}

// Assembly-implemented bulk operations. Each processes n uint64 words.
// dst and src must not be nil when n > 0.

//go:noescape
func simdAnd(dst, src *uint64, n int)

//go:noescape
func simdSrcAndNotDst(dst, src *uint64, n int)

//go:noescape
func simdDstAndNotSrc(dst, src *uint64, n int)

//go:noescape
func simdXor(dst, src *uint64, n int)

//go:noescape
func simdOr(dst, src *uint64, n int)

//go:noescape
func simdPopcnt(dst, src *uint64, n int)

//go:noescape
func simdShl(dst, src *uint64, n int)

//go:noescape
func simdShr(dst, src *uint64, n int)

//go:noescape
func simdTruthTable(dst, src *uint64, n int, op uint8)

//go:noescape
func simdHasHammingMatch(frame *uint64, n int, target uint64, maxDist uint64) bool
