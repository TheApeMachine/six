//go:build amd64

package cpu

import (
	"github.com/theapemachine/six/pkg/errnie"
	"golang.org/x/sys/cpu"
)

// execWordBlock dispatches the body of a word-aligned span operation.
// Hot opcodes are served by AVX2 assembly when AVX2 is available;
// otherwise the portable scalar kernel is used.
func execWordBlock(dst, src []uint64, op uint8) {
	errnie.Trace(
		"cpu.Backend.handleAlu",
		"hw", "simd-accelerated amd64",
		"op", op,
		"dst", dst,
		"src", src,
	)

	n := len(dst)
	if len(src) < n {
		n = len(src)
	}
	if n == 0 {
		return
	}
	dst = dst[:n]
	src = src[:n]

	if !cpu.X86.HasAVX2 {
		execWordBlockScalar(dst, src, op)
		return
	}

	switch op {
	case 0x1: // AND
		simdAnd(&dst[0], &src[0], n)
	case 0x2: // src &^ dst (A ∧ ¬B)
		simdSrcAndNotDst(&dst[0], &src[0], n)
	case 0x3: // COPY
		copy(dst, src)
	case 0x4: // dst &^= src (¬A ∧ B) — VPANDN with swapped operands
		simdDstAndNotSrc(&dst[0], &src[0], n)
	case 0x6: // XOR
		simdXor(&dst[0], &src[0], n)
	case 0x7: // OR
		simdOr(&dst[0], &src[0], n)
	case 0x10: // POPCOUNT (Hamming distance)
		simdPopcnt(&dst[0], &src[0], n)
	case 0x11: // Memory SHL: dst[i] = dst[i] << (src[i] & 63)
		simdShl(&dst[0], &src[0], n)
	case 0x12: // Memory SHR: dst[i] = dst[i] >> (src[i] & 63)
		simdShr(&dst[0], &src[0], n)
	default:
		execWordBlockScalar(dst, src, op)
	}
}

// HasHammingMatch returns true if any word in frame has
// popcount(word ^ target) <= maxDist.
// Uses AVX2 with early exit when available; falls back to scalar otherwise.
func HasHammingMatch(frame []uint64, target uint64, maxDist uint64) bool {
	if len(frame) == 0 {
		return false
	}
	if cpu.X86.HasAVX2 {
		return simdHasHammingMatch(&frame[0], len(frame), target, maxDist)
	}
	for _, w := range frame {
		d := uint64(0)
		xw := w ^ target
		for xw != 0 {
			d++
			xw &= xw - 1
		}
		if d <= maxDist {
			return true
		}
	}
	return false
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
func simdHasHammingMatch(frame *uint64, n int, target uint64, maxDist uint64) bool
