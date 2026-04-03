package cpu

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
LGPExtendedMarker tags a 32-bit LGP slot as a multi-word VSA / control op.

Layout when bit 31 is set:

	[6:0]   extended opcode
	[13:7]  argA — source word index (0–127)
	[20:14] argB — second source / dest word index
	[27:21] argC — rotation amount or roll distance
	[30:28] reserved (must be 0 for forward compatibility)

Legacy slots keep bit 31 clear; decoding matches README (4-bit TT + 14-bit indices).
*/
const LGPExtendedMarker uint32 = 0x80000000

/*
PackExtendedInstruction builds a 32-bit extended LGP slot from opcode and
three 7-bit word indices / immediates.
*/
func PackExtendedInstruction(op uint8, argA, argB, argC int) uint32 {

	return LGPExtendedMarker |
		uint32(op&0x7F) |
		uint32(argA&0x7F)<<7 |
		uint32(argB&0x7F)<<14 |
		uint32(argC&0x7F)<<21
}

/*
Extended VSA / memory opcodes (self-frame only).
*/
const (
	LGPXTokenBindStrip uint8 = 1
	LGPXTokenMaj3      uint8 = 2
	LGPXTokenRotBits   uint8 = 3
	LGPXTokenRolWords  uint8 = 4
	LGPXMemoryLoadMark uint8 = 5
	// LGPXResonatorUnbind XORs a rotated macro-graph buffer into tokens.
	// argB != 0 selects PrevID as pivot key instead of ValueID.
	LGPXResonatorUnbind uint8 = 6
)

/*
DecodeExtendedInstruction extracts fields when instr carries LGPExtendedMarker.
*/
func DecodeExtendedInstruction(instr uint32) (op uint8, argA, argB, argC int, extended bool) {

	if instr&LGPExtendedMarker == 0 {
		return 0, 0, 0, 0, false
	}

	op = uint8(instr & 0x7F)
	argA = int((instr >> 7) & 0x7F)
	argB = int((instr >> 14) & 0x7F)
	argC = int((instr >> 21) & 0x7F)

	return op, argA, argB, argC, true
}

func clampWordIndex(idx int) int {

	if idx < 0 {
		return 0
	}

	if idx > 127 {
		return 127
	}

	return idx
}

func majorityU64(a, b, c uint64) uint64 {

	var out uint64

	for bit := 0; bit < 64; bit++ {
		mask := uint64(1) << bit
		count := 0

		if a&mask != 0 {
			count++
		}

		if b&mask != 0 {
			count++
		}

		if c&mask != 0 {
			count++
		}

		if count >= 2 {
			out |= mask
		}
	}

	return out
}

/*
ExecuteExtendedInstruction applies one extended slot to a single frame.
*/
func ExecuteExtendedInstruction(frame *[128]uint64, instr uint32) {

	op, argA, argB, argC, ok := DecodeExtendedInstruction(instr)
	if !ok {
		return
	}

	argA = clampWordIndex(argA)
	argB = clampWordIndex(argB)
	argC = clampWordIndex(argC)

	tokenBits := int(core.Cfg.Value.Region.Tokens.Bits)
	tokenWords := int((tokenBits + 63) / 64)
	base := core.Cfg.Value.Region.Tokens.Start

	switch op {
	case LGPXTokenBindStrip:

		for offset := 0; offset < tokenWords; offset++ {
			wordIdx := base + offset
			sourceIdx := argA + offset

			if wordIdx >= len(frame) || sourceIdx >= len(frame) {
				break
			}

			frame[wordIdx] ^= frame[sourceIdx]
		}

	case LGPXTokenMaj3:

		for offset := 0; offset < tokenWords; offset++ {
			wordIdx := base + offset
			firstIdx := argA + offset
			secondIdx := argB + offset

			if wordIdx >= len(frame) || firstIdx >= len(frame) || secondIdx >= len(frame) {
				break
			}

			frame[wordIdx] = majorityU64(
				frame[wordIdx],
				frame[firstIdx],
				frame[secondIdx],
			)
		}

	case LGPXTokenRotBits:
		rot := argC & 63
		if rot == 0 {
			return
		}

		for offset := 0; offset < tokenWords; offset++ {
			wordIdx := base + offset

			if wordIdx >= len(frame) {
				break
			}

			frame[wordIdx] = bits.RotateLeft64(frame[wordIdx], rot)
		}

	case LGPXTokenRolWords:
		if tokenWords <= 1 {
			return
		}

		shift := argC % tokenWords
		if shift == 0 {
			return
		}

		var buffer [128]uint64

		for offset := 0; offset < tokenWords; offset++ {
			buffer[offset] = frame[base+offset]
		}

		for offset := 0; offset < tokenWords; offset++ {
			target := (offset + shift) % tokenWords
			frame[base+target] = buffer[offset]
		}

	case LGPXMemoryLoadMark:
		primitive.SetMemoryLoadPending(frame, argA, argB)

	case LGPXResonatorUnbind:
		idWord := core.Cfg.Value.Region.ID.Start
		prevWord := core.Cfg.Value.Region.Prev.Start

		pivot := uint64(0)
		if idWord >= 0 && idWord < len(frame) {
			pivot = frame[idWord]
		}

		if argB != 0 && prevWord >= 0 && prevWord < len(frame) && frame[prevWord] != 0 {
			pivot = frame[prevWord]
		}

		primitive.ApplyResonatorUnbindToTokens(frame, pivot, argC)

	default:
		return
	}
}
