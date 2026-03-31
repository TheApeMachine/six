package cpu

import "math/bits"

// execWordBlockScalar applies a truth-table opcode across aligned slices of
// uint64 words. This is the pure-Go reference implementation. On arm64 the
// compiler auto-vectorises the inner loops to NEON; on amd64 we override
// selected opcodes with AVX2 assembly (see wordblock_amd64.go).
//
// Convention: src = A (source / left operand), dst = B (destination / right
// operand). The result is written into dst. Opcode 0x2 = A &^ B, NOT B &^ A.
func execWordBlockScalar(dst, src []uint64, op uint8) {
	n := len(dst)
	if len(src) < n {
		n = len(src)
	}
	dst = dst[:n]
	src = src[:n]

	switch op {
	case 0x0: // FALSE
		for i := range dst {
			dst[i] = 0
		}
	case 0x1: // A ∧ B
		for i := range dst {
			dst[i] &= src[i]
		}
	case 0x2: // A ∧ ¬B  ← the bug was here: old code did B ∧ ¬A
		for i := range dst {
			dst[i] = src[i] &^ dst[i]
		}
	case 0x3: // A (copy)
		copy(dst, src)
	case 0x4: // ¬A ∧ B
		for i := range dst {
			dst[i] &^= src[i]
		}
	case 0x5: // B (identity — nop)
		// nothing
	case 0x6: // A ⊕ B
		for i := range dst {
			dst[i] ^= src[i]
		}
	case 0x7: // A ∨ B
		for i := range dst {
			dst[i] |= src[i]
		}
	case 0x8: // ¬(A ∨ B)
		for i := range dst {
			dst[i] = ^(src[i] | dst[i])
		}
	case 0x9: // ¬(A ⊕ B)
		for i := range dst {
			dst[i] = ^(src[i] ^ dst[i])
		}
	case 0xA: // ¬B
		for i := range dst {
			dst[i] = ^dst[i]
		}
	case 0xB: // A ∨ ¬B
		for i := range dst {
			dst[i] = src[i] | ^dst[i]
		}
	case 0xC: // ¬A
		for i := range dst {
			dst[i] = ^src[i]
		}
	case 0xD: // ¬A ∨ B
		for i := range dst {
			dst[i] = ^src[i] | dst[i]
		}
	case 0xE: // ¬(A ∧ B)
		for i := range dst {
			dst[i] = ^(src[i] & dst[i])
		}
	case 0xF: // TRUE
		for i := range dst {
			dst[i] = ^uint64(0)
		}
	case 0x10: // POPCOUNT (Hamming distance)
		for i := range dst {
			dst[i] = uint64(bits.OnesCount64(src[i] ^ dst[i]))
		}
	}
}
