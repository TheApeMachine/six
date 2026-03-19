package primitive

import (
	"math/bits"

	config "github.com/theapemachine/six/pkg/system/core"
)

/*
Set activates the prime at index p.
*/
func (value *Value) Set(p int) {
	value.setBlock(p/64, value.Block(p/64)|(1<<(p%64)))
}

/*
OR returns the element-wise OR of two values (their LCM in prime exponent space).
*/
func (value Value) OR(other Value) (lcm Value, err error) {
	if lcm, err = New(); err != nil {
		return Value{}, err
	}

	if err = value.ORInto(other, &lcm); err != nil {
		return Value{}, err
	}

	return
}

/*
ORInto writes the element-wise OR result into destination to avoid per-call
allocation when callers can reuse one Value across iterations.
*/
func (value Value) ORInto(other Value, destination *Value) error {
	if destination == nil || !destination.IsValid() {
		return NewValueError(ValueErrorTypeInvalidDestination)
	}

	destination.SetC0(value.C0() | other.C0())
	destination.SetC1(value.C1() | other.C1())
	destination.SetC2(value.C2() | other.C2())
	destination.SetC3(value.C3() | other.C3())
	destination.SetC4((value.C4() | other.C4()) & 1)
	destination.SetC5(0)
	destination.SetC6(0)
	destination.SetC7(0)

	return nil
}

/*
XOR returns the element-wise XOR of two values (for cancellative superposition).
*/
func (value Value) XOR(other Value) (xor Value, err error) {
	if xor, err = New(); err != nil {
		return Value{}, err
	}

	if err = value.XORInto(other, &xor); err != nil {
		return Value{}, err
	}

	return
}

/*
XORInto writes the element-wise XOR result into destination to avoid per-call
allocation when callers can reuse one Value across iterations.
*/
func (value Value) XORInto(other Value, destination *Value) error {
	if destination == nil || !destination.IsValid() {
		return NewValueError(ValueErrorTypeInvalidDestination)
	}

	destination.SetC0(value.C0() ^ other.C0())
	destination.SetC1(value.C1() ^ other.C1())
	destination.SetC2(value.C2() ^ other.C2())
	destination.SetC3(value.C3() ^ other.C3())
	destination.SetC4((value.C4() ^ other.C4()) & 1)
	destination.SetC5(0)
	destination.SetC6(0)
	destination.SetC7(0)

	return nil
}

/*
AND returns the element-wise AND of two values (their GCD in
prime exponent space), checking allocation errors. Shared factors.
*/
func (value Value) AND(other Value) (gcd Value, err error) {
	if gcd, err = New(); err != nil {
		return Value{}, err
	}

	gcd.SetC0(value.C0() & other.C0())
	gcd.SetC1(value.C1() & other.C1())
	gcd.SetC2(value.C2() & other.C2())
	gcd.SetC3(value.C3() & other.C3())
	gcd.SetC4((value.C4() & other.C4()) & 1)

	return gcd, nil
}

/*
ANDInto writes the element-wise AND result into destination to avoid per-call
allocation when callers can reuse one Value across iterations.
*/
func (value Value) ANDInto(other Value, destination *Value) error {
	if destination == nil || !destination.IsValid() {
		return NewValueError(ValueErrorTypeInvalidDestination)
	}

	destination.SetC0(value.C0() & other.C0())
	destination.SetC1(value.C1() & other.C1())
	destination.SetC2(value.C2() & other.C2())
	destination.SetC3(value.C3() & other.C3())
	destination.SetC4((value.C4() & other.C4()) & 1)
	destination.SetC5(0)
	destination.SetC6(0)
	destination.SetC7(0)

	return nil
}

/*
Hole returns value AND NOT other — bits set in the receiver but not in the argument.
Calling target.Hole(existing) gives the bits that target has but existing lacks.
*/
func (value Value) Hole(other Value) (hole Value, err error) {
	if hole, err = New(); err != nil {
		return Value{}, err
	}

	if err = value.HoleInto(other, &hole); err != nil {
		return Value{}, err
	}

	return
}

/*
HoleInto writes the receiver-minus-argument result into destination to avoid
per-call allocation when callers can reuse one Value across iterations.
*/
func (value Value) HoleInto(other Value, destination *Value) error {
	if destination == nil || !destination.IsValid() {
		return NewValueError(ValueErrorTypeInvalidDestination)
	}

	destination.SetC0(value.C0() &^ other.C0())
	destination.SetC1(value.C1() &^ other.C1())
	destination.SetC2(value.C2() &^ other.C2())
	destination.SetC3(value.C3() &^ other.C3())
	destination.SetC4(value.C4() &^ other.C4())
	destination.SetC5(value.C5() &^ other.C5())
	destination.SetC6(value.C6() &^ other.C6())
	destination.SetC7(value.C7() &^ other.C7())

	return nil
}

/*
Similarity returns the number of shared prime exponents in the 257-bit
core only. Shell bits (upper 63 bits of block 4 and blocks 5–7) are excluded.
*/
func (value *Value) Similarity(other Value) int {
	coreMask4 := uint64(1)

	return bits.OnesCount64(
		value.C0()&other.C0(),
	) + bits.OnesCount64(
		value.C1()&other.C1(),
	) + bits.OnesCount64(
		value.C2()&other.C2(),
	) + bits.OnesCount64(
		value.C3()&other.C3(),
	) + bits.OnesCount64(
		(value.C4()&other.C4())&coreMask4,
	)
}

/*
Bin maps a value to a structural bin 0..255 for indexing phase tables.
Deterministic XOR-fold of the value bits ensures similar values map to nearby bins.
Enables value-native co-occurrence and phase lookup without byte symbols.
*/
func (value *Value) Bin() int {
	seeds := [8]uint64{
		0x9e3779b185ebca87,
		0xc2b2ae3d27d4eb4f,
		0x165667b19e3779f9,
		0x85ebca77c2b2ae63,
		0x27d4eb2f165667c5,
		0x94d049bb133111eb,
		0xd6e8feb86659fd93,
		0xa4093822299f31d1,
	}

	var acc [8]int

	for i := range config.ValueBlocks {
		block := value.Block(i)

		for block != 0 {
			bit := bits.TrailingZeros64(block)
			idx := uint64(i*64 + bit + 1)

			for j := range seeds {
				h := idx*seeds[j] + (idx << uint(j+1))

				if h>>63 == 1 {
					acc[j]++
				} else {
					acc[j]--
				}
			}

			block &= block - 1
		}
	}

	var bin int

	for j := range acc {
		if acc[j] >= 0 {
			bin |= 1 << j
		}
	}

	return bin
}

/*
CoreActiveCount returns the number of active bits in the lower 257-bit Fermat
core only. This deliberately ignores the shell/jacket bits so density and core
energy calculations are not distorted by operator metadata.
*/
func (value Value) CoreActiveCount() int {
	return bits.OnesCount64(
		value.C0(),
	) + bits.OnesCount64(
		value.C1(),
	) + bits.OnesCount64(
		value.C2(),
	) + bits.OnesCount64(
		value.C3(),
	) + bits.OnesCount64(
		value.C4()&1,
	)
}

/*
ShellActiveCount returns the number of active bits in the 255-bit hardware
jacket used for control metadata, carries, and higher-dimensional operators.
*/
func (value Value) ShellActiveCount() int {
	return bits.OnesCount64(
		value.C4()&^uint64(1),
	) + bits.OnesCount64(
		value.C5(),
	) + bits.OnesCount64(
		value.C6(),
	) + bits.OnesCount64(
		value.C7(),
	)
}

/*
ActiveCount returns the number of active bits across the full 512-bit value.
This is useful for total energy/accounting, while CoreActiveCount should be
used whenever the caller explicitly means the GF(257) execution field.
*/
func (value Value) ActiveCount() int {
	return value.CoreActiveCount() + value.ShellActiveCount()
}
