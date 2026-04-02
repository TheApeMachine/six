package pysix

import (
	"errors"
)

var errImmBadIndex = errors.New("pysix.immStep: dst index out of range")

/*
EncodeImm builds a word that applyStep executes as: a[dst] = uint64(imm).
Imm words are distinct from truth-table descriptors (bit 63 set).
*/
func EncodeImm(dst uint8, imm uint16) uint64 {

	return (1 << 63) |
		uint64(dst&0x7F)<<24 |
		uint64(imm)<<8
}

func immStep(a *[FrameWords]uint64, word uint64) error {

	dst := uint8((word >> 24) & 0x7F)
	if uint16(dst) >= FrameWords {
		return errImmBadIndex
	}

	imm := uint16((word >> 8) & 0xFFFF)
	a[dst] = uint64(imm)

	return nil
}
