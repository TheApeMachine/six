package cpu

import (
	"math"
	"unsafe"
)

/*
geometricFrameGeneric is the scalar reference for the PGA lane. AMD64 and
ARM64 route through architecture-specific assembly, and tests compare both
paths against this implementation byte-for-byte.

Frame layout:
  - Signals  word 32, byte 256: output multivector
  - Context  word 40, byte 320: left operand
  - Gradient word 48, byte 384: right operand

Opcode high nibble selects the operation:
  - 0x10 compose:  out = left * right
  - 0x20 sandwich: out = (left * right) * reverse(right)
  - 0x30 reverse:  out = reverse(left)
*/
func geometricFrameGeneric(value unsafe.Pointer, opcode uint64) bool {
	base := (*uint64)(value)
	switch opcode & 0xF0 {
	case 0x10:
		left := lanesAt(base, 320)
		right := lanesAt(base, 384)
		out := geometricProduct(left, right)
		storeLanes(base, 256, out)
		return true
	case 0x20:
		left := lanesAt(base, 320)
		right := lanesAt(base, 384)
		tmp := geometricProduct(left, right)
		rev := reverseLanes(right)
		out := geometricProduct(tmp, rev)
		storeLanes(base, 256, out)
		return true
	case 0x30:
		left := lanesAt(base, 320)
		out := reverseLanes(left)
		storeLanes(base, 256, out)
		return true
	default:
		return false
	}
}

func lanesAt(base *uint64, byteOffset uintptr) [8]float64 {
	ptr := (*[8]float64)(unsafe.Add(unsafe.Pointer(base), byteOffset))
	return *ptr
}

func storeLanes(base *uint64, byteOffset uintptr, lanes [8]float64) {
	ptr := (*[8]float64)(unsafe.Add(unsafe.Pointer(base), byteOffset))
	*ptr = lanes
}

/*
reverseLanes mirrors the assembly's XOR of the sign bit on components 1..6
while keeping components 0 and 7 untouched.
*/
func reverseLanes(in [8]float64) [8]float64 {
	out := in
	for i := 1; i <= 6; i++ {
		out[i] = math.Float64frombits(math.Float64bits(in[i]) ^ 0x8000000000000000)
	}
	return out
}

/*
geometricProduct is a 1:1 port of geometricProductStore in geometric_amd64.s.
Operand order and sign pattern match the assembly exactly so this lane is
numerically equivalent to the SIMD lanes.
*/
func geometricProduct(left, right [8]float64) [8]float64 {
	var out [8]float64

	out[0] = left[0]*right[0] - left[4]*right[4] - left[5]*right[5] - left[6]*right[6]

	out[1] = left[0]*right[1] + left[1]*right[0] -
		left[2]*right[4] + left[3]*right[5] +
		left[4]*right[2] - left[5]*right[3] -
		left[6]*right[7] - left[7]*right[6]

	out[2] = left[0]*right[2] + left[1]*right[4] +
		left[2]*right[0] - left[3]*right[6] -
		left[4]*right[1] - left[5]*right[7] +
		left[6]*right[3] - left[7]*right[5]

	out[3] = left[0]*right[3] - left[1]*right[5] +
		left[2]*right[6] + left[3]*right[0] -
		left[4]*right[7] + left[5]*right[1] -
		left[6]*right[2] - left[7]*right[4]

	out[4] = left[0]*right[4] + left[4]*right[0] +
		left[5]*right[6] - left[6]*right[5]

	out[5] = left[0]*right[5] - left[4]*right[6] +
		left[5]*right[0] + left[6]*right[4]

	out[6] = left[0]*right[6] + left[4]*right[5] -
		left[5]*right[4] + left[6]*right[0]

	out[7] = left[0]*right[7] + left[1]*right[6] +
		left[2]*right[5] + left[3]*right[4] +
		left[4]*right[3] + left[5]*right[2] +
		left[6]*right[1] + left[7]*right[0]

	return out
}
