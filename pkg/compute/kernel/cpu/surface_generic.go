//go:build !amd64 && !arm64

package cpu

import "unsafe"

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
