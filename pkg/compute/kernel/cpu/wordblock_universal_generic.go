//go:build !arm64 && !amd64

package cpu

import (
	"math/bits"
	"unsafe"
)

/*
UniversalBitwise is the generic Go fallback for architectures without a
native SIMD implementation. ARM64 (NEON) and AMD64 (SSE2/AVX2) ship their
own //go:noescape stubs in wordblock_universal_arm64.{go,s} and
wordblock_universal_amd64.{go,s}; this file is built only on platforms
where neither apply.

It executes the resident program directly from the Value's program region:
walks 64-bit instruction words from word 16 onwards, decodes operands and
opcode, and applies the truth-table sweep in place.
*/
func UniversalBitwise(value unsafe.Pointer) {
	if value == nil {
		return
	}

	v := (*[128]uint64)(value)

	// Program region: words 16..31 (16 packed 64-bit instructions). We walk
	// until we hit the first zero word, which is the kernel-side end-of-
	// program sentinel — the same convention the compiler emits.
	for pc := 16; pc < 32; pc++ {
		instr := v[pc]
		if instr == 0 {
			break // End of program
		}

		// Decode the 64-bit instruction word
		// Layout:
		// [0:6]   dstSpan - 1
		// [7:13]  dstStart
		// [14:20] bSpan - 1
		// [21:27] bStart
		// [28:34] aSpan - 1
		// [35:41] aStart
		// [42:45] opcode (4 bits)
		// [46:46] mode (1 bit: 0=accumulate, 1=reduce)

		dstSpan := int(instr&0x7F) + 1
		dstStart := int((instr >> 7) & 0x7F)
		bSpan := int((instr>>14)&0x7F) + 1
		bStart := int((instr >> 21) & 0x7F)
		aSpan := int((instr>>28)&0x7F) + 1
		aStart := int((instr >> 35) & 0x7F)
		op := (instr >> 42) & 0xF
		mode := (instr >> 46) & 0x1

		// Broadcast the 4-bit opcode across 16 rotations (64 bits)
		var opcodeTable uint64
		for rot := 0; rot < 16; rot++ {
			opcodeTable |= (op << uint(rot*4))
		}

		// --- The Sweep Logic ---

		// XOR-fold srcA into four lanes.
		var aLane [4]uint64
		for idx := 0; idx < aSpan; idx++ {
			aLane[idx&3] ^= v[aStart+idx]
		}

		// Signature accumulator: 16 rotations × 4 result-word low bytes = 64 bytes
		var sigBytes [64]byte
		for rot := 0; rot < 16; rot++ {
			rotOp := (opcodeTable >> uint(rot*4)) & 0xF

			m0, m1, m2, m3 := uint64(0), uint64(0), uint64(0), uint64(0)
			if rotOp&0x1 != 0 {
				m0 = ^uint64(0)
			}
			if rotOp&0x2 != 0 {
				m1 = ^uint64(0)
			}
			if rotOp&0x4 != 0 {
				m2 = ^uint64(0)
			}
			if rotOp&0x8 != 0 {
				m3 = ^uint64(0)
			}

			for lane := 0; lane < 4; lane++ {
				bIdx := bStart + ((rot*4)+lane)%bSpan
				a := aLane[lane]
				b := v[bIdx]
				notA, notB := ^a, ^b

				result := (a & b & m0) | (a & notB & m1) | (notA & b & m2) | (notA & notB & m3)
				sigBytes[rot*4+lane] = byte(result)
			}
		}

		// Pack the 64-byte signature into 8 little-endian uint64 signal words
		var sigWords [8]uint64
		for wordIdx := 0; wordIdx < 8; wordIdx++ {
			base := wordIdx * 8
			sigWords[wordIdx] = uint64(sigBytes[base]) | uint64(sigBytes[base+1])<<8 |
				uint64(sigBytes[base+2])<<16 | uint64(sigBytes[base+3])<<24 |
				uint64(sigBytes[base+4])<<32 | uint64(sigBytes[base+5])<<40 |
				uint64(sigBytes[base+6])<<48 | uint64(sigBytes[base+7])<<56
		}

		if mode == 0 { // accumulate
			limit := dstSpan
			if limit > 8 {
				limit = 8
			}
			for idx := 0; idx < limit; idx++ {
				v[dstStart+idx] ^= sigWords[idx]
			}
		} else { // reduce
			var total uint64
			for idx := 0; idx < 8; idx++ {
				total += uint64(bits.OnesCount64(sigWords[idx]))
			}
			v[dstStart] = total
		}
	}
}
