package primitive

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/numeric"
	config "github.com/theapemachine/six/pkg/system/core"
)

/*
Set activates the prime at index p within the 8191-bit core field.
*/
func (value *Value) Set(p int) {
	value.setBlock(p/64, value.Block(p/64)|(1<<(p%64)))
}

/*
Has reports whether bit p is active in the value's core field.
*/
func (value *Value) Has(p int) bool {
	return value.Block(p/64)&(1<<(p%64)) != 0
}

/*
OR returns the element-wise OR of two values (their LCM in prime exponent space).
Operates across the full core; shell blocks are zeroed.
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
ORInto writes the element-wise OR result into destination. Operates on core
blocks only (0..CoreBlocks-1); the last core block is masked to CoreBits%64
active bits. Shell blocks are zeroed.
*/
func (value Value) ORInto(other Value, destination *Value) error {
	if destination == nil || !destination.IsValid() {
		return NewValueError(ValueErrorTypeInvalidDestination)
	}

	lastCore := config.CoreBlocks - 1

	for i := range lastCore {
		destination.setBlock(i, value.Block(i)|other.Block(i))
	}

	destination.setBlock(lastCore,
		(value.Block(lastCore)|other.Block(lastCore))&coreMaskLast,
	)

	for i := config.CoreBlocks; i < config.TotalBlocks; i++ {
		destination.setBlock(i, 0)
	}

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
XORInto writes the element-wise XOR result into destination. Core-only;
shell blocks zeroed.
*/
func (value Value) XORInto(other Value, destination *Value) error {
	if destination == nil || !destination.IsValid() {
		return NewValueError(ValueErrorTypeInvalidDestination)
	}

	lastCore := config.CoreBlocks - 1

	for i := range lastCore {
		destination.setBlock(i, value.Block(i)^other.Block(i))
	}

	destination.setBlock(lastCore,
		(value.Block(lastCore)^other.Block(lastCore))&coreMaskLast,
	)

	for i := config.CoreBlocks; i < config.TotalBlocks; i++ {
		destination.setBlock(i, 0)
	}

	return nil
}

/*
AND returns the element-wise AND of two values (their GCD in
prime exponent space). Shared factors only.
*/
func (value Value) AND(other Value) (gcd Value, err error) {
	if gcd, err = New(); err != nil {
		return Value{}, err
	}

	lastCore := config.CoreBlocks - 1

	for i := range lastCore {
		gcd.setBlock(i, value.Block(i)&other.Block(i))
	}

	gcd.setBlock(lastCore,
		(value.Block(lastCore)&other.Block(lastCore))&coreMaskLast,
	)

	return gcd, nil
}

/*
ANDInto writes the element-wise AND result into destination.
Core-only; shell blocks zeroed.
*/
func (value Value) ANDInto(other Value, destination *Value) error {
	if destination == nil || !destination.IsValid() {
		return NewValueError(ValueErrorTypeInvalidDestination)
	}

	lastCore := config.CoreBlocks - 1

	for i := range lastCore {
		destination.setBlock(i, value.Block(i)&other.Block(i))
	}

	destination.setBlock(lastCore,
		(value.Block(lastCore)&other.Block(lastCore))&coreMaskLast,
	)

	for i := config.CoreBlocks; i < config.TotalBlocks; i++ {
		destination.setBlock(i, 0)
	}

	return nil
}

/*
Hole returns value AND NOT other — bits set in the receiver but not in the argument.
Operates on ALL blocks (core + shell) since hole residue is structurally
meaningful across the entire value.
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
HoleInto writes the receiver-minus-argument result into destination.
Operates on all TotalBlocks since hole residue spans the full value.
*/
func (value Value) HoleInto(other Value, destination *Value) error {
	if destination == nil || !destination.IsValid() {
		return NewValueError(ValueErrorTypeInvalidDestination)
	}

	for i := range config.TotalBlocks {
		destination.setBlock(i, value.Block(i)&^other.Block(i))
	}

	return nil
}

/*
Similarity returns the number of shared prime exponents in the core field only.
Shell blocks are excluded so density and energy calculations are not distorted
by operator metadata.
*/
func (value *Value) Similarity(other Value) int {
	count := 0
	lastCore := config.CoreBlocks - 1

	for i := range lastCore {
		count += bits.OnesCount64(value.Block(i) & other.Block(i))
	}

	count += bits.OnesCount64(
		(value.Block(lastCore) & other.Block(lastCore)) & coreMaskLast,
	)

	return count
}

/*
Bin maps a value to a structural bin 0..255 for indexing phase tables.
Deterministic XOR-fold of the value bits ensures similar values map to nearby bins.
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

	for i := range config.TotalBlocks {
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
CoreActiveCount returns the number of active bits in the 8191-bit core field.
Shell blocks are excluded so density calculations reflect only the GF(8191)
execution space.
*/
func (value Value) CoreActiveCount() int {
	count := 0
	lastCore := config.CoreBlocks - 1

	for i := range lastCore {
		count += bits.OnesCount64(value.Block(i))
	}

	count += bits.OnesCount64(value.Block(lastCore) & coreMaskLast)

	return count
}

/*
ShellActiveCount returns the number of active bits in the shell region
used for control metadata, carries, and higher-dimensional operators.
*/
func (value Value) ShellActiveCount() int {
	lastCore := config.CoreBlocks - 1
	count := bits.OnesCount64(value.Block(lastCore) &^ coreMaskLast)

	for i := config.CoreBlocks; i < config.TotalBlocks; i++ {
		count += bits.OnesCount64(value.Block(i))
	}

	return count
}

/*
ActiveCount returns the number of active bits across the full value
(core + shell). Use CoreActiveCount when you specifically mean the
GF(8191) execution field.
*/
func (value Value) ActiveCount() int {
	return value.CoreActiveCount() + value.ShellActiveCount()
}

/*
coreMaskLast is the bitmask for the final core block. CoreBits = 8191,
and 8191 mod 64 = 63, so the last core block uses bits 0..62 (63 bits).
Bit 63 belongs to the shell.
*/
var coreMaskLast = func() uint64 {
	rem := numeric.CoreBits % 64

	if rem == 0 {
		return ^uint64(0)
	}

	return (1 << rem) - 1
}()
