package primitive

import "math/bits"

/*
MotorError is a typed error for motor algebra failures.
*/
type MotorError string

const (
	ErrMotorNonInvertible MotorError = "motor: non-invertible scale"
)

/*
Error implements the error interface for MotorError.
*/
func (motorErr MotorError) Error() string {
	return string(motorErr)
}

/*
motorEntry holds precomputed scale (product) and translate (sum) for
a single byte value at a specific position in the 128-word field.
256 possible byte values × 1024 byte slots (128 words × 8 bytes).
*/
type motorEntry struct {
	s uint16
	t uint16
}

/*
motorTable precomputes Motor contributions per byte slot. Index as
motorTable[wordIdx][byteIdx][byteValue]. Byte slot (i, j) covers
bit positions i*64 + j*8 through i*64 + j*8 + 7. For byte value b,
the entry stores the product and sum of active prime indices mod 8191.
Zero-byte entries are identity: {s: 1, t: 0}.
*/
var motorTable [Words][8][256]motorEntry

/*
motorMasks clamps the last word to CoreBits; all other words pass through.
*/
var motorMasks [Words]uint64

func init() {
	for i := range Words {
		motorMasks[i] = ^uint64(0)
	}

	motorMasks[Words-1] = LastMask

	for wordIdx := range Words {
		for byteIdx := range 8 {
			for byteVal := range 256 {
				s := uint32(1)
				t := uint32(0)

				for bit := range 8 {
					if byteVal&(1<<bit) == 0 {
						continue
					}

					prime := uint32(wordIdx*64 + byteIdx*8 + bit)

					if prime >= CoreBits {
						continue
					}

					s = (s * prime) % CoreBits
					t = (t + prime) % CoreBits
				}

				motorTable[wordIdx][byteIdx][byteVal] = motorEntry{
					s: uint16(s),
					t: uint16(t),
				}
			}
		}
	}
}

/*
mod8191 reduces x modulo the Mersenne prime 8191 = 2^13 - 1.
One round of shift-and-add plus a conditional subtraction handles
products up to 8190² ≈ 67M.
*/
func mod8191(x uint32) uint32 {
	x = (x >> 13) + (x & 8191)

	if x >= 8191 {
		x -= 8191
	}

	return x
}

/*
ApplyMotor applies f(p)=scale*p+translate (mod 8191).
*/
func ApplyMotor(scale, translate, position uint16) uint16 {
	return uint16(
		(uint32(scale)*uint32(position) + uint32(translate)) % uint32(CoreBits),
	)
}

/*
ComposeMotor composes f2(f1(p)) for two affine operators in GF(8191).
*/
func ComposeMotor(scale1, translate1, scale2, translate2 uint16) (uint16, uint16) {
	composedScale := uint16((uint32(scale2) * uint32(scale1)) % uint32(CoreBits))
	composedTranslate := uint16(
		(uint32(scale2)*uint32(translate1) + uint32(translate2)) % uint32(CoreBits),
	)

	return composedScale, composedTranslate
}

/*
InvertMotor returns inverse operator g(p)=invScale*p+invTranslate (mod 8191)
such that g(f(p))=p for every position.
*/
func InvertMotor(scale, translate uint16) (invScale, invTranslate uint16, err error) {
	invScale, err = modInverse8191(scale)
	if err != nil {
		return 0, 0, err
	}

	invTranslate = uint16(
		(uint32(CoreBits) - (uint32(invScale)*uint32(translate))%uint32(CoreBits)) % uint32(CoreBits),
	)

	return invScale, invTranslate, nil
}

/*
modInverse8191 computes multiplicative inverse in GF(8191) using the
extended Euclidean algorithm.
*/
func modInverse8191(value uint16) (uint16, error) {
	t, newT := int32(0), int32(1)
	r, newR := int32(CoreBits), int32(value)

	for newR != 0 {
		quotient := r / newR
		t, newT = newT, t-quotient*newT
		r, newR = newR, r-quotient*newR
	}

	if r > 1 {
		return 0, ErrMotorNonInvertible
	}

	if t < 0 {
		t += int32(CoreBits)
	}

	return uint16(t), nil
}

/*
Motor derives the affine operator f(p) = scale·p + translate (mod 8191)
from the field. Scale is the product of active prime indices mod 8191.
Translate is their sum mod 8191. Scale zero normalizes to identity.

Hybrid strategy: sparse words (<4 bits set) use bit-scanning with
Mersenne mod8191; dense words (>=4 bits set) use the precomputed
motorTable with dual ILP accumulators. This wins or ties at every
measured density from 10 to 4000 active bits.
*/
func (value *Value) Motor() (scale, translate uint16) {
	s1, s2 := uint32(1), uint32(1)
	t1, t2 := uint32(0), uint32(0)

	for i := range Words {
		word := value[i] & motorMasks[i]

		if word == 0 {
			continue
		}

		if bits.OnesCount64(word) < 4 {
			for word != 0 {
				bit := bits.TrailingZeros64(word)
				prime := uint32(i*64 + bit)

				s1 = mod8191(s1 * prime)
				t1 = mod8191(t1 + prime)

				word &= word - 1
			}

			continue
		}

		b0 := byte(word)
		b1 := byte(word >> 8)
		b2 := byte(word >> 16)
		b3 := byte(word >> 24)
		b4 := byte(word >> 32)
		b5 := byte(word >> 40)
		b6 := byte(word >> 48)
		b7 := byte(word >> 56)

		e0 := motorTable[i][0][b0]
		e1 := motorTable[i][1][b1]
		e2 := motorTable[i][2][b2]
		e3 := motorTable[i][3][b3]
		e4 := motorTable[i][4][b4]
		e5 := motorTable[i][5][b5]
		e6 := motorTable[i][6][b6]
		e7 := motorTable[i][7][b7]

		s1 = mod8191(s1 * uint32(e0.s))
		s2 = mod8191(s2 * uint32(e1.s))
		s1 = mod8191(s1 * uint32(e2.s))
		s2 = mod8191(s2 * uint32(e3.s))
		s1 = mod8191(s1 * uint32(e4.s))
		s2 = mod8191(s2 * uint32(e5.s))
		s1 = mod8191(s1 * uint32(e6.s))
		s2 = mod8191(s2 * uint32(e7.s))

		t1 = mod8191(
			t1 +
				uint32(e0.t) +
				uint32(e2.t) +
				uint32(e4.t) +
				uint32(e6.t),
		)

		t2 = mod8191(
			t2 +
				uint32(e1.t) +
				uint32(e3.t) +
				uint32(e5.t) +
				uint32(e7.t),
		)
	}

	s := mod8191(s1 * s2)
	t := mod8191(t1 + t2)

	if s == 0 {
		s = 1
	}

	return uint16(s), uint16(t)
}
