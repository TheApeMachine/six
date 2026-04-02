package pysix

import (
	"errors"
	"fmt"

	"github.com/theapemachine/six/pkg/compute/kernel/cpu"
	"github.com/theapemachine/six/pkg/core"
)

/*
Run executes a compiled program on frame using stepwise.RunScalar. The frame
must be zero-initialized; prologue instructions set slotZero and slotOnes.
*/
func Run(frame *[FrameWords]uint64, prog []uint64) error {

	return RunScalar(frame, prog)
}

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
			errors.New("pysix.DecodeStep: IMM words are not TT descriptors")
	}

	if word>>33 != 0 {
		return 0, 0, 0, 0, false, false,
			errors.New("pysix.DecodeStep: reserved bits 33:62 must be zero")
	}

	op = uint8(word & 0xFF)
	idxA = uint8((word >> 8) & 0x7F)
	idxB = uint8((word >> 16) & 0x7F)
	idxDst = uint8((word >> 24) & 0x7F)
	leftFromB = word&(1<<31) != 0
	rightFromB = word&(1<<32) != 0

	if uint16(idxA) >= FrameWords || uint16(idxB) >= FrameWords || uint16(idxDst) >= FrameWords {
		return 0, 0, 0, 0, false, false,
			errors.New("pysix.DecodeStep: index >= FrameWords")
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
			return 0, errors.New("pysix: partner frame required for operand")
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
		return errors.New("pysix.RunPair: nil frame")
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
		return errors.New("pysix.RunEmbedded: no room for header and descriptors")
	}

	hdr := ctx[base]
	stepCount, ok := EmbeddedDescriptorCount(hdr)
	if !ok {
		return errors.New("pysix.RunEmbedded: missing or invalid stepwise header word")
	}

	if stepCount > maxAfterHeader {
		return fmt.Errorf("pysix.RunEmbedded: invalid header: claimed %d steps but only %d fit after header", stepCount, maxAfterHeader)
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
		return errors.New("pysix.RunEmbeddedPair: nil frame")
	}

	base := EmbeddedProgramBase()
	maxAfterHeader := FrameWords - base - 1

	if maxAfterHeader <= 0 {
		return errors.New("pysix.RunEmbeddedPair: no room for batch header and descriptors")
	}

	hdr := a[base]
	stepCount, ok := EmbeddedDescriptorCount(hdr)
	if !ok {
		return errors.New("pysix.RunEmbeddedPair: missing or invalid stepwise header word")
	}

	if stepCount > maxAfterHeader {
		return fmt.Errorf("pysix.RunEmbeddedPair: invalid header: claimed %d steps but only %d fit after header", stepCount, maxAfterHeader)
	}

	for step := 0; step < stepCount; step++ {
		if err := applyStep(a, b, a[base+1+step]); err != nil {
			return err
		}
	}

	return nil
}

/*
FrameWords is the fixed word count of one Value frame; must match primitive.Words.
*/
const FrameWords = 128

/*
DefaultProgramWordBase matches the embedded program band in cmd/cfg/config.yml
when viper has not overridden value.region.program.start.
*/
const DefaultProgramWordBase = 76

/*
ProgramWordsAvailable returns how many uint64 step descriptors fit in the tail
of a frame when the embedded program starts at DefaultProgramWordBase.
*/
func ProgramWordsAvailable() int {
	return FrameWords - DefaultProgramWordBase
}

/*
EmbeddedProgramBase returns the configured program word index, falling back to
DefaultProgramWordBase when unset or out of range.
*/
func EmbeddedProgramBase() int {
	start := core.Cfg.Value.Region.Program.Start

	if start <= 0 || start >= FrameWords {
		return DefaultProgramWordBase
	}

	return start
}

/*
EmbeddedStepCount returns the number of consecutive words in the program band
starting at EmbeddedProgramBase through the end of the frame (header + payload).
*/
func EmbeddedStepCount() int {
	return FrameWords - EmbeddedProgramBase()
}

/*
EmbeddedHeaderMagic sits in bits 48–63 of the first word of the embedded program
band. Legacy packed 16-bit instructions are extremely unlikely to match this
exact pattern with bits 16–47 clear, so the backend can route stepwise vs RISC.
*/
const EmbeddedHeaderMagic uint16 = 0x5A17

/*
PackEmbeddedHeader builds the word stored at ctx[EmbeddedProgramBase()] before
descriptor words. stepCount is how many uint64 descriptor words follow; 0 means
run zero steps.
*/
func PackEmbeddedHeader(stepCount uint16) uint64 {

	return uint64(EmbeddedHeaderMagic)<<48 | uint64(stepCount)
}

/*
ValidEmbeddedHeader reports whether word is a well-formed stepwise band header.
*/
func ValidEmbeddedHeader(word uint64) bool {

	if uint16(word>>48) != EmbeddedHeaderMagic {
		return false
	}

	if (word>>16)&0xFFFFFFFF != 0 {
		return false
	}

	return true
}

/*
EmbeddedDescriptorCount returns how many descriptor words follow the header, or
false if word is not a header.
*/
func EmbeddedDescriptorCount(word uint64) (steps int, ok bool) {

	if !ValidEmbeddedHeader(word) {
		return 0, false
	}

	return int(word & 0xFFFF), true
}

/*
DetectEmbeddedStepwise returns true when frame a has a valid stepwise header at
the configured program base.
*/
func DetectEmbeddedStepwise(a *[FrameWords]uint64) bool {

	if a == nil {
		return false
	}

	base := EmbeddedProgramBase()
	if base < 0 || base >= FrameWords {
		return false
	}

	return ValidEmbeddedHeader(a[base])
}
