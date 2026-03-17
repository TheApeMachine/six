package data

import (
	"math/bits"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/errnie"
	config "github.com/theapemachine/six/pkg/system/core"
)

/*
OR returns the element-wise OR of two values (their LCM in prime exponent space).
*/
func (value *Value) OR(other Value) Value {
	lcm := MustNewValue()

	lcm.SetC0(value.C0() | other.C0())
	lcm.SetC1(value.C1() | other.C1())
	lcm.SetC2(value.C2() | other.C2())
	lcm.SetC3(value.C3() | other.C3())
	lcm.SetC4((value.C4() | other.C4()) & 1)

	return lcm
}

/*
XOR returns the element-wise XOR of two values (for cancellative superposition).
*/
func (value Value) XOR(other Value) Value {
	xor := MustNewValue()

	xor.SetC0(value.C0() ^ other.C0())
	xor.SetC1(value.C1() ^ other.C1())
	xor.SetC2(value.C2() ^ other.C2())
	xor.SetC3(value.C3() ^ other.C3())
	xor.SetC4((value.C4() ^ other.C4()) & 1)

	return xor
}

/*
ANDErr returns the element-wise AND of two values (their GCD in
prime exponent space), checking allocation errors. Shared factors.
*/
func (value *Value) ANDErr(other Value) (Value, error) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		return Value{}, err
	}

	state := errnie.NewState("data/andErr")

	gcd := errnie.Guard(state, func() (Value, error) {
		return NewValue(seg)
	})

	if state.Failed() {
		return Value{}, state.Err()
	}

	gcd.setBlock(0, value.block(0)&other.block(0))
	gcd.setBlock(1, value.block(1)&other.block(1))
	gcd.setBlock(2, value.block(2)&other.block(2))
	gcd.setBlock(3, value.block(3)&other.block(3))
	gcd.setBlock(4, value.block(4)&other.block(4))
	gcd.setBlock(5, value.block(5)&other.block(5))
	gcd.setBlock(6, value.block(6)&other.block(6))
	gcd.setBlock(7, value.block(7)&other.block(7))

	return gcd, nil
}

/*
AND returns the element-wise AND of two values (their GCD in
prime exponent space). Shared factors.
*/
func (value *Value) AND(other Value) Value {
	state := errnie.NewState("data/and")

	result := errnie.Guard(state, func() (Value, error) {
		return value.ANDErr(other)
	})

	if state.Failed() {
		panic(state.Err())
	}

	return result
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
		block := value.block(i)

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
Similarity returns the number of shared prime exponents (popcount of AND).
*/
func (value *Value) Similarity(other Value) int {
	return bits.OnesCount64(
		value.C0()&other.C0(),
	) + bits.OnesCount64(
		value.C1()&other.C1(),
	) + bits.OnesCount64(
		value.C2()&other.C2(),
	) + bits.OnesCount64(
		value.C3()&other.C3(),
	) + bits.OnesCount64(
		value.C4()&other.C4(),
	) + bits.OnesCount64(
		value.C5()&other.C5(),
	) + bits.OnesCount64(
		value.C6()&other.C6(),
	) + bits.OnesCount64(
		value.C7()&other.C7(),
	)
}

/*
Hole returns value AND NOT other — bits set in the receiver but not in the argument.
Calling target.Hole(existing) gives the bits that target has but existing lacks.
*/
func (value *Value) Hole(other Value) Value {
	hole := MustNewValue()
	hole.setBlock(0, value.block(0)&^other.block(0))
	hole.setBlock(1, value.block(1)&^other.block(1))
	hole.setBlock(2, value.block(2)&^other.block(2))
	hole.setBlock(3, value.block(3)&^other.block(3))
	hole.setBlock(4, value.block(4)&^other.block(4))
	hole.setBlock(5, value.block(5)&^other.block(5))
	hole.setBlock(6, value.block(6)&^other.block(6))
	hole.setBlock(7, value.block(7)&^other.block(7))
	return hole
}

/*
ValueLCM returns the element-wise OR of values — the LCM in prime exponent space.
Used for aggregating span values (words, sentences, n-grams).
*/
func ValueLCM(values []Value) (lcm Value) {
	lcm = MustNewValue()

	var c0, c1, c2, c3, c4 uint64

	for _, ch := range values {
		c0 |= ch.C0()
		c1 |= ch.C1()
		c2 |= ch.C2()
		c3 |= ch.C3()
		c4 |= ch.C4()
	}

	lcm.SetC0(c0)
	lcm.SetC1(c1)
	lcm.SetC2(c2)
	lcm.SetC3(c3)
	lcm.SetC4(c4 & 1)
	lcm.SetC5(0)
	lcm.SetC6(0)
	lcm.SetC7(0)

	return lcm
}

/*
CoreActiveCount returns the number of active bits in the lower 257-bit Fermat
core only. This deliberately ignores the shell/jacket bits so density and core
energy calculations are not distorted by operator metadata.
*/
func (value Value) CoreActiveCount() (n int) {
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
func (value Value) ActiveCount() (n int) {
	return value.CoreActiveCount() + value.ShellActiveCount()
}
