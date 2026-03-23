package cpu

import (
	"math/bits"
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
)

/*
mod8191 fast reduction for the 8191 Mersenne prime.
*/
func mod8191(x uint32) uint32 {
	x = (x >> 13) + (x & 8191)

	if x >= 8191 {
		x -= 8191
	}

	return x
}

/*
deriveMotor extracts (scale, translate) from a single Value's active bit pattern.
*/
func deriveMotor(val *[primitive.Words]uint64) (uint32, uint32) {
	s := uint32(1)
	t := uint32(0)

	for i := range primitive.Words {
		w := val[i]

		if i == primitive.Words-1 {
			w &= primitive.LastMask
		}

		for w != 0 {
			bit := bits.TrailingZeros64(w)
			p := uint32(i*64 + bit)
			s = mod8191(s * p)
			t = mod8191(t + p)
			w &= w - 1
		}
	}

	if s == 0 {
		s = 1
	}

	return s, t
}

/*
invertMotor computes the GF(8191) multiplicative inverse via extended Euclidean.
*/
func invertMotor(scale, translate uint32) (uint32, uint32) {
	tVal, newT := int32(0), int32(1)
	r, newR := int32(8191), int32(scale)

	for newR != 0 {
		quotient := r / newR

		tVal, newT = newT, tVal-quotient*newT
		r, newR = newR, r-quotient*newR
	}

	if tVal < 0 {
		tVal += 8191
	}

	invScale := uint32(tVal)
	invTranslate := (8191 - (invScale*translate)%8191) % 8191

	return invScale, invTranslate
}

/*
applyMotor remaps every active bit in src through f(p) = scale*p + translate (mod 8191).
*/
func applyMotor(src *[primitive.Words]uint64, dst *[primitive.Words]uint64, scale, translate uint32) {
	for i := range primitive.Words {
		dst[i] = 0
	}

	for i := range primitive.Words {
		w := src[i]

		if i == primitive.Words-1 {
			w &= primitive.LastMask
		}

		for w != 0 {
			bit := bits.TrailingZeros64(w)
			p := uint32(i*64 + bit)
			mapped := mod8191(scale*p + translate)
			dst[mapped/64] |= 1 << (mapped % 64)
			w &= w - 1
		}
	}

	dst[primitive.Words-1] &= primitive.LastMask
}

/*
MotorApply derives motor(A) and applies it to B for N Value pairs.
*/
func (backend *Backend) MotorApply(a, b, dst unsafe.Pointer, numValues uint32) error {
	as := unsafe.Slice((*[primitive.Words]uint64)(a), numValues)
	bs := unsafe.Slice((*[primitive.Words]uint64)(b), numValues)
	ds := unsafe.Slice((*[primitive.Words]uint64)(dst), numValues)

	for v := range numValues {
		s, t := deriveMotor(&as[v])
		applyMotor(&bs[v], &ds[v], s, t)
	}

	return nil
}

/*
MotorInvert derives inverse motor(A) and applies it to B for N Value pairs.
*/
func (backend *Backend) MotorInvert(a, b, dst unsafe.Pointer, numValues uint32) error {
	as := unsafe.Slice((*[primitive.Words]uint64)(a), numValues)
	bs := unsafe.Slice((*[primitive.Words]uint64)(b), numValues)
	ds := unsafe.Slice((*[primitive.Words]uint64)(dst), numValues)

	for v := range numValues {
		s, t := deriveMotor(&as[v])
		invS, invT := invertMotor(s, t)
		applyMotor(&bs[v], &ds[v], invS, invT)
	}

	return nil
}

/*
MotorCompose composes motor(A) then motor(B) and applies the composed motor to B.
*/
func (backend *Backend) MotorCompose(a, b, dst unsafe.Pointer, numValues uint32) error {
	as := unsafe.Slice((*[primitive.Words]uint64)(a), numValues)
	bs := unsafe.Slice((*[primitive.Words]uint64)(b), numValues)
	ds := unsafe.Slice((*[primitive.Words]uint64)(dst), numValues)

	for v := range numValues {
		sA, tA := deriveMotor(&as[v])
		sB, tB := deriveMotor(&bs[v])
		compS := mod8191(sB * sA)
		compT := mod8191(sB*tA + tB)
		applyMotor(&bs[v], &ds[v], compS, compT)
	}

	return nil
}
