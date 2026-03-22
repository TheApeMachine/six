package cpu

import (
	"math/bits"
	"runtime"
	"unsafe"

	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/transport"
)

/*
Backend is the CPU kernel backend. It mirrors the Metal/CUDA dispatch
surface so that every GPU kernel has a verified CPU fallback.
*/
type Backend struct {
	*transport.Stream
}

/*
NewBackend returns a CPU Backend.
*/
func NewBackend() *Backend {
	return &Backend{
		Stream: transport.NewStream(),
	}
}

/*
Available returns the number of logical CPU cores.
*/
func (backend *Backend) Available() (int, error) {
	return runtime.NumCPU(), nil
}

/* ─── Bitwise Operations ──────────────────────────────────────────────── */

/*
BitwiseOr computes a[i] | b[i] for N Value pairs.
*/
func (backend *Backend) BitwiseOr(a, b, dst unsafe.Pointer, numValues uint32) error {
	as, bs, ds := valueSlices(a, b, dst, numValues)

	for v := range numValues {
		base := v * primitive.Words

		for i := uint32(0); i < primitive.Words; i += 8 {
			ds[base+i+0] = as[base+i+0] | bs[base+i+0]
			ds[base+i+1] = as[base+i+1] | bs[base+i+1]
			ds[base+i+2] = as[base+i+2] | bs[base+i+2]
			ds[base+i+3] = as[base+i+3] | bs[base+i+3]
			ds[base+i+4] = as[base+i+4] | bs[base+i+4]
			ds[base+i+5] = as[base+i+5] | bs[base+i+5]
			ds[base+i+6] = as[base+i+6] | bs[base+i+6]
			ds[base+i+7] = as[base+i+7] | bs[base+i+7]
		}

		ds[base+primitive.Words-1] &= primitive.LastMask
	}

	return nil
}

/*
BitwiseAnd computes a[i] & b[i] for N Value pairs.
*/
func (backend *Backend) BitwiseAnd(a, b, dst unsafe.Pointer, numValues uint32) error {
	as, bs, ds := valueSlices(a, b, dst, numValues)

	for v := range numValues {
		base := v * primitive.Words

		for i := uint32(0); i < primitive.Words; i += 8 {
			ds[base+i+0] = as[base+i+0] & bs[base+i+0]
			ds[base+i+1] = as[base+i+1] & bs[base+i+1]
			ds[base+i+2] = as[base+i+2] & bs[base+i+2]
			ds[base+i+3] = as[base+i+3] & bs[base+i+3]
			ds[base+i+4] = as[base+i+4] & bs[base+i+4]
			ds[base+i+5] = as[base+i+5] & bs[base+i+5]
			ds[base+i+6] = as[base+i+6] & bs[base+i+6]
			ds[base+i+7] = as[base+i+7] & bs[base+i+7]
		}

		ds[base+primitive.Words-1] &= primitive.LastMask
	}

	return nil
}

/*
BitwiseXor computes a[i] ^ b[i] for N Value pairs.
*/
func (backend *Backend) BitwiseXor(a, b, dst unsafe.Pointer, numValues uint32) error {
	as, bs, ds := valueSlices(a, b, dst, numValues)

	for v := range numValues {
		base := v * primitive.Words

		for i := uint32(0); i < primitive.Words; i += 8 {
			ds[base+i+0] = as[base+i+0] ^ bs[base+i+0]
			ds[base+i+1] = as[base+i+1] ^ bs[base+i+1]
			ds[base+i+2] = as[base+i+2] ^ bs[base+i+2]
			ds[base+i+3] = as[base+i+3] ^ bs[base+i+3]
			ds[base+i+4] = as[base+i+4] ^ bs[base+i+4]
			ds[base+i+5] = as[base+i+5] ^ bs[base+i+5]
			ds[base+i+6] = as[base+i+6] ^ bs[base+i+6]
			ds[base+i+7] = as[base+i+7] ^ bs[base+i+7]
		}

		ds[base+primitive.Words-1] &= primitive.LastMask
	}

	return nil
}

/*
BitwiseAndNot computes a[i] &^ b[i] (material nonimplication) for N Value pairs.
*/
func (backend *Backend) BitwiseAndNot(a, b, dst unsafe.Pointer, numValues uint32) error {
	as, bs, ds := valueSlices(a, b, dst, numValues)

	for v := range numValues {
		base := v * primitive.Words

		for i := uint32(0); i < primitive.Words; i += 8 {
			ds[base+i+0] = as[base+i+0] &^ bs[base+i+0]
			ds[base+i+1] = as[base+i+1] &^ bs[base+i+1]
			ds[base+i+2] = as[base+i+2] &^ bs[base+i+2]
			ds[base+i+3] = as[base+i+3] &^ bs[base+i+3]
			ds[base+i+4] = as[base+i+4] &^ bs[base+i+4]
			ds[base+i+5] = as[base+i+5] &^ bs[base+i+5]
			ds[base+i+6] = as[base+i+6] &^ bs[base+i+6]
			ds[base+i+7] = as[base+i+7] &^ bs[base+i+7]
		}

		ds[base+primitive.Words-1] &= primitive.LastMask
	}

	return nil
}

/*
BitwiseNand computes ^(a[i] & b[i]) for N Value pairs.
*/
func (backend *Backend) BitwiseNand(a, b, dst unsafe.Pointer, numValues uint32) error {
	as, bs, ds := valueSlices(a, b, dst, numValues)

	for v := range numValues {
		base := v * primitive.Words

		for i := uint32(0); i < primitive.Words; i += 8 {
			ds[base+i+0] = ^(as[base+i+0] & bs[base+i+0])
			ds[base+i+1] = ^(as[base+i+1] & bs[base+i+1])
			ds[base+i+2] = ^(as[base+i+2] & bs[base+i+2])
			ds[base+i+3] = ^(as[base+i+3] & bs[base+i+3])
			ds[base+i+4] = ^(as[base+i+4] & bs[base+i+4])
			ds[base+i+5] = ^(as[base+i+5] & bs[base+i+5])
			ds[base+i+6] = ^(as[base+i+6] & bs[base+i+6])
			ds[base+i+7] = ^(as[base+i+7] & bs[base+i+7])
		}

		ds[base+primitive.Words-1] &= primitive.LastMask
	}

	return nil
}

/*
BitwiseNor computes ^(a[i] | b[i]) for N Value pairs.
*/
func (backend *Backend) BitwiseNor(a, b, dst unsafe.Pointer, numValues uint32) error {
	as, bs, ds := valueSlices(a, b, dst, numValues)

	for v := range numValues {
		base := v * primitive.Words

		for i := uint32(0); i < primitive.Words; i += 8 {
			ds[base+i+0] = ^(as[base+i+0] | bs[base+i+0])
			ds[base+i+1] = ^(as[base+i+1] | bs[base+i+1])
			ds[base+i+2] = ^(as[base+i+2] | bs[base+i+2])
			ds[base+i+3] = ^(as[base+i+3] | bs[base+i+3])
			ds[base+i+4] = ^(as[base+i+4] | bs[base+i+4])
			ds[base+i+5] = ^(as[base+i+5] | bs[base+i+5])
			ds[base+i+6] = ^(as[base+i+6] | bs[base+i+6])
			ds[base+i+7] = ^(as[base+i+7] | bs[base+i+7])
		}

		ds[base+primitive.Words-1] &= primitive.LastMask
	}

	return nil
}

/*
BitwiseXnor computes ^(a[i] ^ b[i]) for N Value pairs.
*/
func (backend *Backend) BitwiseXnor(a, b, dst unsafe.Pointer, numValues uint32) error {
	as, bs, ds := valueSlices(a, b, dst, numValues)

	for v := range numValues {
		base := v * primitive.Words

		for i := uint32(0); i < primitive.Words; i += 8 {
			ds[base+i+0] = ^(as[base+i+0] ^ bs[base+i+0])
			ds[base+i+1] = ^(as[base+i+1] ^ bs[base+i+1])
			ds[base+i+2] = ^(as[base+i+2] ^ bs[base+i+2])
			ds[base+i+3] = ^(as[base+i+3] ^ bs[base+i+3])
			ds[base+i+4] = ^(as[base+i+4] ^ bs[base+i+4])
			ds[base+i+5] = ^(as[base+i+5] ^ bs[base+i+5])
			ds[base+i+6] = ^(as[base+i+6] ^ bs[base+i+6])
			ds[base+i+7] = ^(as[base+i+7] ^ bs[base+i+7])
		}

		ds[base+primitive.Words-1] &= primitive.LastMask
	}

	return nil
}

/*
BitwiseConverseNonimplication computes b[i] &^ a[i] for N Value pairs.
*/
func (backend *Backend) BitwiseConverseNonimplication(a, b, dst unsafe.Pointer, numValues uint32) error {
	as, bs, ds := valueSlices(a, b, dst, numValues)

	for v := range numValues {
		base := v * primitive.Words

		for i := uint32(0); i < primitive.Words; i += 8 {
			ds[base+i+0] = bs[base+i+0] &^ as[base+i+0]
			ds[base+i+1] = bs[base+i+1] &^ as[base+i+1]
			ds[base+i+2] = bs[base+i+2] &^ as[base+i+2]
			ds[base+i+3] = bs[base+i+3] &^ as[base+i+3]
			ds[base+i+4] = bs[base+i+4] &^ as[base+i+4]
			ds[base+i+5] = bs[base+i+5] &^ as[base+i+5]
			ds[base+i+6] = bs[base+i+6] &^ as[base+i+6]
			ds[base+i+7] = bs[base+i+7] &^ as[base+i+7]
		}

		ds[base+primitive.Words-1] &= primitive.LastMask
	}

	return nil
}

/*
BitwiseNot computes ^a[i] for N Values (unary).
*/
func (backend *Backend) BitwiseNot(a, dst unsafe.Pointer, numValues uint32) error {
	total := uint32(primitive.Words) * numValues
	as := unsafe.Slice((*uint64)(a), total)
	ds := unsafe.Slice((*uint64)(dst), total)

	for v := range numValues {
		base := v * primitive.Words

		for i := uint32(0); i < primitive.Words; i += 8 {
			ds[base+i+0] = ^as[base+i+0]
			ds[base+i+1] = ^as[base+i+1]
			ds[base+i+2] = ^as[base+i+2]
			ds[base+i+3] = ^as[base+i+3]
			ds[base+i+4] = ^as[base+i+4]
			ds[base+i+5] = ^as[base+i+5]
			ds[base+i+6] = ^as[base+i+6]
			ds[base+i+7] = ^as[base+i+7]
		}

		ds[base+primitive.Words-1] &= primitive.LastMask
	}

	return nil
}

/* ─── Motor Operations ────────────────────────────────────────────────── */

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

/* ─── Structural Geometry ─────────────────────────────────────────────── */

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

/* ─── Helpers ─────────────────────────────────────────────────────────── */

/*
valueSlices projects three raw pointers into flat uint64 slices for binary ops.
*/
func valueSlices(a, b, dst unsafe.Pointer, numValues uint32) ([]uint64, []uint64, []uint64) {
	total := uint32(primitive.Words) * numValues

	return unsafe.Slice((*uint64)(a), total),
		unsafe.Slice((*uint64)(b), total),
		unsafe.Slice((*uint64)(dst), total)
}
