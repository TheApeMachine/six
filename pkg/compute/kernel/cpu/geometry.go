package cpu

import (
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
RollLeft circular-shifts all core bits left by shift positions for N Values.
*/
func (backend *Backend) RollLeft(src, dst unsafe.Pointer, shift, numValues uint32) error {
	ss := unsafe.Slice((*[primitive.Words]uint64)(src), numValues)
	ds := unsafe.Slice((*[primitive.Words]uint64)(dst), numValues)

	for v := uint32(0); v < numValues; v++ {
		rollLeftOne(&ss[v], &ds[v], shift)
	}

	return nil
}

func rollLeftOne(src, dst *[primitive.Words]uint64, shift uint32) {
	s := ((shift % primitive.CoreBits) + primitive.CoreBits) % primitive.CoreBits

	if s == 0 {
		*dst = *src
		return
	}

	r := primitive.CoreBits - s

	wShiftL := s / 64
	bShiftL := s % 64
	wShiftR := r / 64
	bShiftR := r % 64

	var tmp [primitive.Words]uint64

	if bShiftL == 0 {
		for i := wShiftL; i < primitive.Words; i++ {
			tmp[i] = src[i-wShiftL]
		}
	} else {
		tmp[wShiftL] = src[0] << bShiftL
		for i := wShiftL + 1; i < uint32(primitive.Words); i++ {
			tmp[i] = (src[i-wShiftL] << bShiftL) | (src[i-wShiftL-1] >> (64 - bShiftL))
		}
	}

	if bShiftR == 0 {
		for i := uint32(0); i < uint32(primitive.Words)-wShiftR; i++ {
			tmp[i] |= src[i+wShiftR]
		}
	} else {
		for i := uint32(0); i < uint32(primitive.Words)-wShiftR-1; i++ {
			tmp[i] |= (src[i+wShiftR] >> bShiftR) | (src[i+wShiftR+1] << (64 - bShiftR))
		}

		tmp[uint32(primitive.Words)-1-wShiftR] |= src[primitive.Words-1] >> bShiftR
	}

	tmp[primitive.Words-1] &= primitive.LastMask
	*dst = tmp
}
