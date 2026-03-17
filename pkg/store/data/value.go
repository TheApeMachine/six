package data

import (
	"fmt"
	"math/bits"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/theapemachine/six/pkg/errnie"
	config "github.com/theapemachine/six/pkg/system/core"
)

/*
NeutralValue allocates a lexical-free native value. The value starts with an
identity affine operator so it behaves like a tiny local program even before
any lexical observable is projected onto it.
*/
func NeutralValue() Value {
	value := MustNewValue()
	value.SetAffine(1, 0)

	return value
}

/*
MustNewValue allocates a fresh zero-valued Value, panicking on allocation failure.
*/
func MustNewValue() Value {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		panic(fmt.Errorf("allocation failed: %w", err))
	}
	value, err := NewValue(seg)
	if err != nil {
		panic(fmt.Errorf("allocation failed: %w", err))
	}
	return value
}

/*
MaskValue returns a control-plane marker used to denote an unresolved gap or
masked region in a sequence without colliding with any lexical BaseValue.
*/
func MaskValue() Value {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))

	if err != nil {
		panic(fmt.Errorf("MaskValue allocation failed: %w", err))
	}

	state := errnie.NewState("data/maskValue")

	value := errnie.Guard(state, func() (Value, error) {
		return NewValue(seg)
	})

	if state.Failed() {
		panic(state.Err())
	}

	value.Set(config.Numeric.VocabSize)

	return value
}

/*
CopyFrom copies all 8 words from src into the receiver.
Replaces the repeated SetC0..SetC7 call pattern at every call site.
*/
func (value *Value) CopyFrom(src Value) {
	for i := range 8 {
		value.setBlock(i, src.block(i))
	}
}

/*
ValueSliceToList packs a Go slice of Values into a Cap'n Proto Value_List.
*/
func ValueSliceToList(values []Value) (Value_List, error) {
	_, seg, err := capnp.NewMessage(capnp.MultiSegment(nil))
	if err != nil {
		return Value_List{}, err
	}

	state := errnie.NewState("data/valueSliceToList")

	list := errnie.Guard(state, func() (Value_List, error) {
		return NewValue_List(seg, int32(len(values)))
	})

	if state.Failed() {
		return Value_List{}, state.Err()
	}

	for i, cc := range values {
		dst := list.At(i)
		dst.CopyFrom(cc)
	}

	return list, nil
}

/*
ValueListToSlice copies each entry into a freshly allocated Value and returns the slice.
*/
func ValueListToSlice(list Value_List) ([]Value, error) {
	out := make([]Value, list.Len())

	_, seg, err := capnp.NewMessage(capnp.MultiSegment(nil))
	if err != nil {
		return nil, err
	}

	state := errnie.NewState("data/valueListToSlice")

	for i := 0; i < list.Len(); i++ {
		src := list.At(i)

		value := errnie.Guard(state, func() (Value, error) {
			return NewValue(seg)
		})

		value.CopyFrom(src)
		out[i] = value
	}

	if state.Failed() {
		return nil, state.Err()
	}

	return out, nil
}

/*
Block returns the raw uint64 word at index i (0-7).
It delegates to the unexported value.block method.
*/
func (value *Value) Block(i int) uint64 {
	return value.block(i)
}

func (value *Value) block(i int) uint64 {
	switch i {
	case 0:
		return value.C0()
	case 1:
		return value.C1()
	case 2:
		return value.C2()
	case 3:
		return value.C3()
	case 4:
		return value.C4()
	case 5:
		return value.C5()
	case 6:
		return value.C6()
	case 7:
		return value.C7()
	default:
		return 0
	}
}

func (value *Value) setBlock(i int, v uint64) {
	switch i {
	case 0:
		value.SetC0(v)
	case 1:
		value.SetC1(v)
	case 2:
		value.SetC2(v)
	case 3:
		value.SetC3(v)
	case 4:
		value.SetC4(v)
	case 5:
		value.SetC5(v)
	case 6:
		value.SetC6(v)
	case 7:
		value.SetC7(v)
	}
}

/*
Has checks if the prime at index p is active in the value.
*/
func (value *Value) Has(p int) bool {
	return (value.block(p/64) & (1 << (p % 64))) != 0
}

/*
Set activates the prime at index p.
*/
func (value *Value) Set(p int) {
	value.setBlock(p/64, value.block(p/64)|(1<<(p%64)))
}

/*
Clear deactivates the prime at index p.
*/
func (value *Value) Clear(p int) {
	value.setBlock(p/64, value.block(p/64)&^(1<<(p%64)))
}

/*
ActivePrimeIndices returns a list of all active prime indices in the value's
core bit-field. This represents the geometric affine signature points.
*/
func (value *Value) ActivePrimeIndices() []int {
	var indices []int
	for blockIdx := range config.ValueBlocks {
		block := value.block(blockIdx)

		for block != 0 {
			bitIdx := bits.TrailingZeros64(block)
			primeIdx := blockIdx*64 + bitIdx

			if primeIdx < 257 {
				indices = append(indices, primeIdx)
			}

			block &^= 1 << bitIdx
		}
	}

	return indices
}

/*
ValuePrimeIndices returns the prime indices (0..NBasis-1) that are set in the value.
Used for debug output (which primes were assigned).
*/
func ValuePrimeIndices(value *Value) []int {
	var out []int

	for i := range config.ValueBlocks {
		block := value.block(i)

		for block != 0 {
			bitIdx := bits.TrailingZeros64(block)
			primeIdx := i*64 + bitIdx

			if primeIdx < config.NBasis {
				out = append(out, primeIdx)
			}

			block &= block - 1
		}
	}

	return out
}

/*
ShannonDensity returns the fraction of the 257 logical core bits that are active.
The Sequencer uses this to force a boundary before the value saturates.
Above ~0.40 (103 bits) the core field loses discriminative power. Shell bits do
not count toward this threshold because they live in the hardware jacket, not in
the Fermat execution field itself.
*/
func (value Value) ShannonDensity() float64 {
	return float64(value.CoreActiveCount()) / 257.0
}
