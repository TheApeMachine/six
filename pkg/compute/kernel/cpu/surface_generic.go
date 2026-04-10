//go:build !amd64 && !arm64

package cpu

import (
	"math/bits"
	"unsafe"
)

/*
universalBitwise applies the truth table across 64 words of A and B
surface data, writing 64 result bytes packed 8-per-word into dst (8 words).
*/
func universalBitwise(dst *uint64, a, b, m0, m1, m2, m3 *uint64) {
	dstSlice := (*[8]uint64)(unsafe.Pointer(dst))
	aSlice := (*[64]uint64)(unsafe.Pointer(a))
	bSlice := (*[64]uint64)(unsafe.Pointer(b))
	m0Slice := (*[64]uint64)(unsafe.Pointer(m0))
	m1Slice := (*[64]uint64)(unsafe.Pointer(m1))
	m2Slice := (*[64]uint64)(unsafe.Pointer(m2))
	m3Slice := (*[64]uint64)(unsafe.Pointer(m3))

	for i := range 8 {
		dstSlice[i] = 0
	}

	for i := range 64 {
		ai := aSlice[i]
		bi := bSlice[i]
		result := (ai & bi & m0Slice[i]) |
			(ai & ^bi & m1Slice[i]) |
			(^ai & bi & m2Slice[i]) |
			(^ai & ^bi & m3Slice[i])

		sigWord := i / 8
		sigShift := uint((i % 8) * 8)
		dstSlice[sigWord] |= (result & 0xFF) << sigShift
	}
}

/*
universalBitwiseV2 reads directly from the Value's pre-compiled
layout. A is at words 0-3, opcode at word 16 (program region),
B rotations at words 32+, signals written to words 24-31 to match
the AVX2 / NEON kernels.

The programmer package must have already expanded B rotations
into the reserved region before calling this.
*/
func universalBitwiseV2(value *uint64, numRotations int) {
	v := (*[128]uint64)(unsafe.Pointer(value))

	opcode := uint8(v[16] & 0xF)
	mask0 := -uint64(opcode & 1)
	mask1 := -uint64((opcode >> 1) & 1)
	mask2 := -uint64((opcode >> 2) & 1)
	mask3 := -uint64((opcode >> 3) & 1)

	for i := range 8 {
		v[24+i] = 0
	}

	for rot := range numRotations {
		bOff := 32 + rot*4

		for word := range 4 {
			idx := rot*4 + word
			ai := v[word]
			bi := v[bOff+word]

			result := (ai & bi & mask0) |
				(ai & ^bi & mask1) |
				(^ai & bi & mask2) |
				(^ai & ^bi & mask3)

			sigWord := idx / 8
			sigShift := uint((idx % 8) * 8)
			v[24+sigWord] |= (result & 0xFF) << sigShift
		}
	}
}

/*
batchAffinityDistances writes Hamming distances from an 8-word query to each
of count contiguous 8-word candidate vectors into out. amd64 / arm64 use
vectorised assembly; this path matches the same definition for 32-bit ARM and
any other GOARCH that does not select those files.
*/
func batchAffinityDistances(query *uint64, candidates *uint64, count int, out *uint32) {
	if count <= 0 {
		return
	}

	queryWords := (*[8]uint64)(unsafe.Pointer(query))
	candidateWords := unsafe.Slice(candidates, count*8)
	distances := unsafe.Slice(out, count)

	for candidateIdx := range count {
		base := candidateIdx * 8
		sum := 0

		for word := range 8 {
			sum += bits.OnesCount64(queryWords[word] ^ candidateWords[base+word])
		}

		distances[candidateIdx] = uint32(sum)
	}
}
