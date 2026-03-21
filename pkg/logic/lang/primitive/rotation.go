package primitive

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/numeric"
	config "github.com/theapemachine/six/pkg/system/core"
)

/*
RotationSeed derives a structural affine seed from the value itself.
Unlike a popcount-only mapping, this uses the actual active prime layout so
distinct values with identical density can still drive different rotations.
Operates over the full core field (CoreBlocks) with modular arithmetic in
GF(8191).
*/
func (value Value) RotationSeed() (uint16, uint16) {
	if value.ActiveCount() == 0 {
		return 1, 0
	}

	var accA uint32 = 1
	var accB uint32

	for blockIdx := range config.CoreBlocks {
		block := value.Block(blockIdx)

		if block == 0 {
			continue
		}

		mix := uint32(block^(block>>29)^(block>>43)) & 0x1FFF
		accA = numeric.MersenneReduce(accA*131 + mix + uint32(blockIdx+1)*17)
		accB = numeric.MersenneReduce(
			accB*137 + mix + uint32(bits.OnesCount64(block))*29 + uint32(blockIdx+1)*31,
		)

		for block != 0 {
			bitIdx := bits.TrailingZeros64(block)
			primeIdx := blockIdx*64 + bitIdx

			if primeIdx >= numeric.CoreBits {
				block &= block - 1
				continue
			}

			prime := uint32(primeIdx + 1)
			accA = numeric.MersenneReduce(accA + prime*prime + prime*23 + uint32(bitIdx+1)*7)
			accB = numeric.MersenneReduce(accB + prime*67 + uint32(bitIdx+1)*13)

			block &= block - 1
		}
	}

	if accA == 0 {
		accA = 1
	}

	return uint16(accA), uint16(accB)
}

/*
RollLeft circular-shifts the value within the CoreBits logical width.
Binds sequential position to geometry before superposition.
*/
func (value Value) RollLeft(shift int) Value {
	if shift == 0 {
		return value
	}

	out, err := New()

	if err != nil {
		panic("RollLeft: " + err.Error())
	}

	value.RollLeftInto(shift, &out)

	return out
}

/*
RollLeftInto circular-shifts the value within the CoreBits logical width and
writes the result into destination, avoiding allocation when callers can reuse
storage across iterations.
*/
func (value Value) RollLeftInto(shift int, destination *Value) {
	for i := range config.TotalBlocks {
		destination.setBlock(i, 0)
	}

	shift = shift % numeric.CoreBits

	if shift == 0 {
		destination.CopyFrom(value)
		return
	}

	for i := range config.CoreBlocks {
		block := value.Block(i)

		for block != 0 {
			bitIdx := uint16(bits.TrailingZeros64(block))
			primeIdx := i*64 + int(bitIdx)

			if primeIdx < numeric.CoreBits {
				newPrimeIdx := (primeIdx + shift) % numeric.CoreBits
				destination.Set(newPrimeIdx)
			}

			block &= block - 1
		}
	}
}

/*
Rotate3D applies all three GF(8191) axes in sequence.
X (Translation): p → (p + 1) mod 8191
Y (Dilation):    p → (g·p) mod 8191  where g is the primitive root
Z (Affine):      p → (g·p + 1) mod 8191
*/
func (value *Value) Rotate3D() Value {
	out, err := New()

	if err != nil {
		panic("Rotate3D: " + err.Error())
	}

	g := int(numeric.FieldPrimitive)

	for i := range config.CoreBlocks {
		block := value.Block(i)

		for block != 0 {
			bitIdx := uint16(bits.TrailingZeros64(block))
			primeIdx := i*64 + int(bitIdx)

			if primeIdx < numeric.CoreBits {
				p := (primeIdx + 1) % numeric.CoreBits
				p = (g * p) % numeric.CoreBits
				p = (g*p + 1) % numeric.CoreBits

				out.Set(p)
			}

			block &= block - 1
		}
	}

	return out
}
