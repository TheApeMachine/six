package stepwise

import (
	"errors"

	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
)

/*
EncodeStep packs op in bits 0–7 (truth-table 0x0–0xF or extended ALU such as 0x13 ADD).
*/
func EncodeStep(op, idxA, idxB, idxDst uint8) uint64 {

	return EncodeStepFrames(op, idxA, idxB, idxDst, false, false)
}

/*
EncodeStepFrames packs a descriptor. When leftFromB is set, the left operand is
b[idxA]; otherwise a[idxA]. When rightFromB is set, the right operand is b[idxB];
otherwise a[idxB]. The result is always written to a[idxDst].

Bits: 0:7 opcode, 8:14 idxA, 16:22 idxB, 24:30 idxDst, 31 leftFromB,
32 rightFromB. Bits 33:62 must be zero. Bit 63 reserved for IMM encoding.
*/
func EncodeStepFrames(op, idxA, idxB, idxDst uint8, leftFromB, rightFromB bool) uint64 {

	word := uint64(op&0xFF) |
		uint64(idxA&0x7F)<<8 |
		uint64(idxB&0x7F)<<16 |
		uint64(idxDst&0x7F)<<24

	if leftFromB {
		word |= 1 << 31
	}

	if rightFromB {
		word |= 1 << 32
	}

	return word
}

/*
DecodeStep unpacks a descriptor and validates encoding.
*/
func DecodeStep(word uint64) (
	op, idxA, idxB, idxDst uint8,
	leftFromB, rightFromB bool,
	err error,
) {

	if word&(1<<63) != 0 {
		return 0, 0, 0, 0, false, false,
			errors.New("stepwise.DecodeStep: IMM words are not TT descriptors")
	}

	if word>>33 != 0 {
		return 0, 0, 0, 0, false, false,
			errors.New("stepwise.DecodeStep: reserved bits 33:62 must be zero")
	}

	op = uint8(word & 0xFF)
	idxA = uint8((word >> 8) & 0x7F)
	idxB = uint8((word >> 16) & 0x7F)
	idxDst = uint8((word >> 24) & 0x7F)
	leftFromB = word&(1<<31) != 0
	rightFromB = word&(1<<32) != 0

	if uint16(idxA) >= FrameWords || uint16(idxB) >= FrameWords || uint16(idxDst) >= FrameWords {
		return 0, 0, 0, 0, false, false,
			errors.New("stepwise.DecodeStep: index >= FrameWords")
	}

	return op, idxA, idxB, idxDst, leftFromB, rightFromB, nil
}

func loadWord(
	a, b *[FrameWords]uint64,
	idx uint8,
	fromB bool,
) (uint64, error) {

	if fromB {
		if b == nil {
			return 0, errors.New("stepwise: partner frame required for operand")
		}

		return b[idx], nil
	}

	return a[idx], nil
}

func applyStep(
	a, b *[FrameWords]uint64,
	word uint64,
) error {

	if word&(1<<63) != 0 {
		return immStep(a, word)
	}

	op, idxA, idxB, idxDst, leftFromB, rightFromB, decodeErr := DecodeStep(word)
	if decodeErr != nil {
		return decodeErr
	}

	left, errLeft := loadWord(a, b, idxA, leftFromB)
	if errLeft != nil {
		return errLeft
	}

	right, errRight := loadWord(a, b, idxB, rightFromB)
	if errRight != nil {
		return errRight
	}

	a[idxDst] = cpu.ExecWord(op, left, right)

	return nil
}

/*
RunScalar executes every descriptor in program in order against a single frame.
Partner operand flags must be clear or b must be supplied; use RunPair when
using frame B.
*/
func RunScalar(ctx *[FrameWords]uint64, program []uint64) error {

	for step := range program {
		if err := applyStep(ctx, nil, program[step]); err != nil {
			return err
		}
	}

	return nil
}

/*
RunPair runs an explicit program against frames a (destination) and b (partner
operand source when descriptor flags request it).
*/
func RunPair(a, b *[FrameWords]uint64, program []uint64) error {

	if a == nil || b == nil {
		return errors.New("stepwise.RunPair: nil frame")
	}

	for step := range program {
		if err := applyStep(a, b, program[step]); err != nil {
			return err
		}
	}

	return nil
}

/*
RunEmbedded loads descriptors from ctx’s embedded program band and runs them
against ctx only (same as scalar over that band).
*/
func RunEmbedded(ctx *[FrameWords]uint64) error {

	base := EmbeddedProgramBase()
	maxAfterHeader := FrameWords - base - 1

	if maxAfterHeader <= 0 {
		return errors.New("stepwise.RunEmbedded: no room for header and descriptors")
	}

	hdr := ctx[base]
	stepCount, ok := EmbeddedDescriptorCount(hdr)
	if !ok {
		return errors.New("stepwise.RunEmbedded: missing or invalid stepwise header word")
	}

	if stepCount > maxAfterHeader {
		stepCount = maxAfterHeader
	}

	for step := 0; step < stepCount; step++ {
		if err := applyStep(ctx, nil, ctx[base+1+step]); err != nil {
			return err
		}
	}

	return nil
}

/*
RunEmbeddedPair loads descriptors from a’s embedded program band and executes
each step with partner b available for leftFromB/rightFromB operand fetches.
Results are written only into a.
*/
func RunEmbeddedPair(a, b *[FrameWords]uint64) error {

	if a == nil || b == nil {
		return errors.New("stepwise.RunEmbeddedPair: nil frame")
	}

	base := EmbeddedProgramBase()
	maxAfterHeader := FrameWords - base - 1

	if maxAfterHeader <= 0 {
		return errors.New("stepwise.RunEmbeddedPair: no room for batch header and descriptors")
	}

	hdr := a[base]
	stepCount, ok := EmbeddedDescriptorCount(hdr)
	if !ok {
		return errors.New("stepwise.RunEmbeddedPair: missing or invalid stepwise header word")
	}

	if stepCount > maxAfterHeader {
		stepCount = maxAfterHeader
	}

	for step := 0; step < stepCount; step++ {
		if err := applyStep(a, b, a[base+1+step]); err != nil {
			return err
		}
	}

	return nil
}
