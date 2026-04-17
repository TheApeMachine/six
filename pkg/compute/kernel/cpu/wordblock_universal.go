package cpu

import (
	"math/bits"
	"unsafe"
)

/*
universalBitwiseSweep runs one 16-rotation truth-table sweep against the Value,
reading operand A from value[aStart..aStart+aSpan) folded to 4 lanes, B from
value[bStart..bStart+bSpan) tiled cyclically across the 16 rotations, and
writing the 64-byte LSH signature back to value[dstStart..dstStart+dstSpan)
under the requested mode.

The kernel touches exactly the three regions the caller names: it never
reads or writes words outside [aStart..aStart+aSpan), [bStart..bStart+bSpan),
and [dstStart..dstStart+dstSpan). That is what makes the DSL's arbitrary
region refs safe — running a program on a Value no longer clobbers tokens,
context, gradient, properties, or any region that is not explicitly the dst.

mode selects the writeback fold: 0 XOR-accumulates the 8 signature words
into dst[0..min(8,dstSpan)) so successive program lines can layer state on
the same region, and 1 popcounts the full 8-word signature and writes the
total scalar into dst[0] (leaving dst[1..] untouched so reduce lines can
coexist with accumulate lines on the same dst span).

opcodeTable is the 16-nibble rotation opcode schedule packed into one
uint64 — nibble k at bit (k*4). Legacy single-opcode programs broadcast
one nibble across all 16 slots so the per-rotation decode matches the
old fixed-op kernel bit-for-bit.
*/
func universalBitwiseSweep(
	value *uint64,
	aStart int,
	aSpan int,
	bStart int,
	bSpan int,
	dstStart int,
	dstSpan int,
	mode int,
	opcodeTable uint64,
) {
	if value == nil || aSpan <= 0 || bSpan <= 0 || dstSpan <= 0 {
		return
	}

	if aStart < 0 || bStart < 0 || dstStart < 0 {
		return
	}

	if aStart+aSpan > 128 || bStart+bSpan > 128 || dstStart+dstSpan > 128 {
		return
	}

	values := (*[128]uint64)(unsafe.Pointer(value))

	// XOR-fold srcA into four lanes. When aSpan < 4 the unused lanes stay
	// zero; when aSpan > 4 every additional word folds back into lane
	// (idx mod 4) so every bit of the A span contributes to the sweep.
	var aLane [4]uint64

	for idx := range aSpan {
		aLane[idx&3] ^= values[aStart+idx]
	}

	// Signature accumulator: 16 rotations × 4 result-word low bytes =
	// 64 bytes = 8 uint64 signal words. Backed as a byte array so the
	// scatter is a cheap offset write per rotation.
	var sigBytes [64]byte

	for rot := range 16 {
		op := (opcodeTable >> uint(rot*4)) & 0xF

		m0 := uint64(0)

		if op&0x1 != 0 {
			m0 = ^uint64(0)
		}

		m1 := uint64(0)

		if op&0x2 != 0 {
			m1 = ^uint64(0)
		}

		m2 := uint64(0)

		if op&0x4 != 0 {
			m2 = ^uint64(0)
		}

		m3 := uint64(0)

		if op&0x8 != 0 {
			m3 = ^uint64(0)
		}

		for lane := 0; lane < 4; lane++ {
			// Cyclic B index — rotation k reads the 4-word window
			// starting at (k*4) mod bSpan so spans smaller than 64
			// words tile seamlessly across the full 16-rotation sweep.
			bIdx := bStart + ((rot*4)+lane)%bSpan

			a := aLane[lane]
			b := values[bIdx]
			notA := ^a
			notB := ^b

			result := (a & b & m0) |
				(a & notB & m1) |
				(notA & b & m2) |
				(notA & notB & m3)

			sigBytes[rot*4+lane] = byte(result)
		}
	}

	// Pack the 64-byte signature into 8 little-endian uint64 signal words
	// so writeback and reduce can operate one word at a time.
	var sigWords [8]uint64

	for wordIdx := range 8 {
		base := wordIdx * 8

		sigWords[wordIdx] = uint64(sigBytes[base]) |
			uint64(sigBytes[base+1])<<8 |
			uint64(sigBytes[base+2])<<16 |
			uint64(sigBytes[base+3])<<24 |
			uint64(sigBytes[base+4])<<32 |
			uint64(sigBytes[base+5])<<40 |
			uint64(sigBytes[base+6])<<48 |
			uint64(sigBytes[base+7])<<56
	}

	if mode == 0 {
		limit := dstSpan

		if limit > 8 {
			limit = 8
		}

		for idx := 0; idx < limit; idx++ {
			values[dstStart+idx] ^= sigWords[idx]
		}

		return
	}

	var total uint64

	for idx := range 8 {
		total += uint64(bits.OnesCount64(sigWords[idx]))
	}

	values[dstStart] = total
}
