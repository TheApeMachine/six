package primitive

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/errnie"
	config "github.com/theapemachine/six/pkg/system/core"
)

/*
RotationSeed derives a structural affine seed from the value itself.
Unlike a popcount-only mapping, this uses the actual active prime layout so
distinct values with identical density can still drive different rotations.
*/
func (value Value) RotationSeed() (uint16, uint16) {
	if value.ActiveCount() == 0 {
		return 1, 0
	}

	var accA uint32 = 1
	var accB uint32

	for blockIdx := range config.ValueBlocks {
		block := value.Block(blockIdx)

		if block == 0 {
			continue
		}

		mix := uint32(block^(block>>29)^(block>>43)) & 0x1FFFF
		accA = (accA*131 + mix + uint32(blockIdx+1)*17) % 257
		accB = (accB*137 + mix + uint32(
			bits.OnesCount64(block),
		)*29 + uint32(blockIdx+1)*31) % 257

		for block != 0 {
			bitIdx := bits.TrailingZeros64(block)
			primeIdx := blockIdx*64 + bitIdx

			if primeIdx >= 257 {
				block &= block - 1
				continue
			}

			prime := uint32(primeIdx + 1)
			accA = (accA + prime*prime + prime*23 + uint32(bitIdx+1)*7) % 257
			accB = (accB + prime*67 + uint32(bitIdx+1)*13) % 257

			block &= block - 1
		}
	}

	if accA == 0 {
		accA = 1
	}

	return uint16(accA), uint16(accB % 257)
}

/*
RollLeft circular-shifts the value within the 257-bit logical width.
Binds sequential position to geometry before superposition.
*/
func (value Value) RollLeft(shift int) Value {
	state := errnie.NewState("primitive/rotation/rollLeft")

	if shift == 0 {
		return value
	}

	const logicalBits = 257 // CubeFaces

	out := errnie.Guard(state, func() (Value, error) {
		return New()
	})

	shift = shift % logicalBits

	// Fast sparse-array permutation within the 257-bit logical width
	for i := range config.ValueBlocks {
		block := value.Block(i)

		for block != 0 {
			bitIdx := uint16(bits.TrailingZeros64(block))
			primeIdx := i*64 + int(bitIdx)

			if primeIdx < logicalBits {
				newPrimeIdx := (primeIdx + shift) % logicalBits
				out.Set(newPrimeIdx)
			}

			block &= block - 1
		}
	}

	return out
}

/*
Rotate3D applies all three GF(257) axes in sequence.
X (Translation): p → (p + 1) mod 257
Y (Dilation):    p → (3·p) mod 257
Z (Affine):      p → (3·p + 1) mod 257
Combined orbit ~17M states. 3 is a primitive root of 257.
*/
func (value *Value) Rotate3D() Value {
	state := errnie.NewState("primitive/rotation/rotate3D")

	const logicalBits = 257

	out := errnie.Guard(state, func() (Value, error) {
		return New()
	})

	for i := range config.ValueBlocks {
		block := value.Block(i)

		for block != 0 {
			bitIdx := uint16(bits.TrailingZeros64(block))
			primeIdx := i*64 + int(bitIdx)

			if primeIdx < logicalBits {
				p := (primeIdx + 1) % logicalBits
				p = (3 * p) % logicalBits
				p = (3*p + 1) % logicalBits

				out.Set(p)
			}

			block &= block - 1
		}
	}

	return out
}
