package primitive

import (
	"math/bits"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/errnie"
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
func (value Value) OR(other Value) (Value, error) {
	state := errnie.NewState("primitive/bitwise/or")

	lcm := errnie.Guard(state, func() (Value, error) {
		return New()
	})

	lcm.SetC0(value.C0() | other.C0())
	lcm.SetC1(value.C1() | other.C1())
	lcm.SetC2(value.C2() | other.C2())
	lcm.SetC3(value.C3() | other.C3())
	lcm.SetC4((value.C4() | other.C4()) & 1)

	return lcm, state.Err()
}

/*
XOR returns the element-wise XOR of two values (for cancellative superposition).
*/
func (value Value) XOR(other Value) (Value, error) {
	state := errnie.NewState("primitive/bitwise/xor")

	xor := errnie.Guard(state, func() (Value, error) {
		return New()
	})

	xor.SetC0(value.C0() ^ other.C0())
	xor.SetC1(value.C1() ^ other.C1())
	xor.SetC2(value.C2() ^ other.C2())
	xor.SetC3(value.C3() ^ other.C3())
	xor.SetC4((value.C4() ^ other.C4()) & 1)

	return xor, state.Err()
}

/*
AND returns the element-wise AND of two values (their GCD in
prime exponent space), checking allocation errors. Shared factors.
*/
func (value Value) AND(other Value) (Value, error) {
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

	coreMask4 := uint64(1)
	gcd.setBlock(0, value.Block(0)&other.Block(0))
	gcd.setBlock(1, value.Block(1)&other.Block(1))
	gcd.setBlock(2, value.Block(2)&other.Block(2))
	gcd.setBlock(3, value.Block(3)&other.Block(3))
	gcd.setBlock(4, (value.Block(4)&other.Block(4))&coreMask4)
	gcd.setBlock(5, 0)
	gcd.setBlock(6, 0)
	gcd.setBlock(7, 0)

	return gcd, nil
}

/*
Hole returns value AND NOT other — bits set in the receiver but not in the argument.
Calling target.Hole(existing) gives the bits that target has but existing lacks.
*/
func (value Value) Hole(other Value) (Value, error) {
	state := errnie.NewState("primitive/bitwise/hole")

	hole := errnie.Guard(state, func() (Value, error) {
		return New()
	})

	for i := range 8 {
		hole = errnie.Guard(state, func() (Value, error) {
			return hole.setBlock(i, value.Block(i)&^other.Block(i))
		})
	}

	return hole, state.Err()
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
