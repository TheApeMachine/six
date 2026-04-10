//go:build !amd64 && !arm64

package cpu

import (
	"math/bits"
	"unsafe"
)

// HammingMatch returns true if any word in frame has
// popcount(word ^ target) <= maxDist.
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

/*
affinityPopcount is the portable reference for the ARM64 / AMD64 SIMD
primitives. Bit 0 of the fifth word is the only meaningful lane — the
rest of that word is garbage that must not enter the total, matching
AffinityLastWordMask on the Go side.
*/
func affinityPopcount(vec *uint64) uint32 {
	words := (*[5]uint64)(unsafe.Pointer(vec))

	total := bits.OnesCount64(words[0]) +
		bits.OnesCount64(words[1]) +
		bits.OnesCount64(words[2]) +
		bits.OnesCount64(words[3]) +
		int(words[4]&1)

	return uint32(total)
}

/*
affinityCoupling writes popcount(a & b) to out[0] and popcount(a | b)
to out[1], matching the SIMD kernels. The fifth-word tail is folded as
a single-bit AND/OR so 257-bit semantics are preserved.
*/
func affinityCoupling(a *uint64, b *uint64, out *uint32) {
	aWords := (*[5]uint64)(unsafe.Pointer(a))
	bWords := (*[5]uint64)(unsafe.Pointer(b))

	intersection := bits.OnesCount64(aWords[0]&bWords[0]) +
		bits.OnesCount64(aWords[1]&bWords[1]) +
		bits.OnesCount64(aWords[2]&bWords[2]) +
		bits.OnesCount64(aWords[3]&bWords[3]) +
		int((aWords[4]&bWords[4])&1)

	union := bits.OnesCount64(aWords[0]|bWords[0]) +
		bits.OnesCount64(aWords[1]|bWords[1]) +
		bits.OnesCount64(aWords[2]|bWords[2]) +
		bits.OnesCount64(aWords[3]|bWords[3]) +
		int((aWords[4]|bWords[4])&1)

	results := (*[2]uint32)(unsafe.Pointer(out))
	results[0] = uint32(intersection)
	results[1] = uint32(union)
}
