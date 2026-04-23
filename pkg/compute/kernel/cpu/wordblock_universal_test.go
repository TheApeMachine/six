package cpu

import (
	"math/bits"
	"math/rand"
	"testing"
	"unsafe"

	"github.com/theapemachine/six/pkg/compute/program"
)

// universalBitwiseRef is the canonical Go reference implementation of the
// in-band VM. The native ARM64 / AMD64 assemblies must match this output bit
// for bit on every Value frame and every program. The body is a verbatim copy
// of wordblock_universal_generic.go so it stays in sync architecturally; the
// only difference is the build tag (this file is compiled everywhere).
func universalBitwiseRef(value unsafe.Pointer) {
	if value == nil {
		return
	}
	v := (*[128]uint64)(value)

	for pc := 16; pc < 32; pc++ {
		instr := v[pc]
		if instr == 0 {
			break
		}

		dstSpan := int(instr&0x7F) + 1
		dstStart := int((instr >> 7) & 0x7F)
		bSpan := int((instr>>14)&0x7F) + 1
		bStart := int((instr >> 21) & 0x7F)
		aSpan := int((instr>>28)&0x7F) + 1
		aStart := int((instr >> 35) & 0x7F)
		op := (instr >> 42) & 0xF
		mode := (instr >> 46) & 0x3
		imm := (instr >> 48) & 0xFFFF

		if mode == 2 { // cmov
			if v[bStart] != 0 {
				for idx := 0; idx < dstSpan; idx++ {
					srcIdx := aStart + (idx % aSpan)
					v[dstStart+idx] = v[srcIdx]
				}
			}
			continue
		}

		if mode == 3 { // imm
			a := v[aStart]
			b := imm
			notA, notB := ^a, ^b

			m0, m1, m2, m3 := uint64(0), uint64(0), uint64(0), uint64(0)
			if op&0x1 != 0 {
				m0 = ^uint64(0)
			}
			if op&0x2 != 0 {
				m1 = ^uint64(0)
			}
			if op&0x4 != 0 {
				m2 = ^uint64(0)
			}
			if op&0x8 != 0 {
				m3 = ^uint64(0)
			}

			v[dstStart] = (a & b & m0) | (a & notB & m1) | (notA & b & m2) | (notA & notB & m3)
			continue
		}

		var aLane [4]uint64
		for idx := 0; idx < aSpan; idx++ {
			aLane[idx&3] ^= v[aStart+idx]
		}

		m0, m1, m2, m3 := uint64(0), uint64(0), uint64(0), uint64(0)
		if op&0x1 != 0 {
			m0 = ^uint64(0)
		}
		if op&0x2 != 0 {
			m1 = ^uint64(0)
		}
		if op&0x4 != 0 {
			m2 = ^uint64(0)
		}
		if op&0x8 != 0 {
			m3 = ^uint64(0)
		}

		var sigBytes [64]byte
		for rot := 0; rot < 16; rot++ {
			for lane := 0; lane < 4; lane++ {
				bIdx := bStart + ((rot*4)+lane)%bSpan
				a := aLane[lane]
				b := v[bIdx]
				notA, notB := ^a, ^b
				result := (a & b & m0) | (a & notB & m1) | (notA & b & m2) | (notA & notB & m3)
				sigBytes[rot*4+lane] = byte(result)
			}
		}

		var sigWords [8]uint64
		for wordIdx := 0; wordIdx < 8; wordIdx++ {
			base := wordIdx * 8
			sigWords[wordIdx] = uint64(sigBytes[base]) | uint64(sigBytes[base+1])<<8 |
				uint64(sigBytes[base+2])<<16 | uint64(sigBytes[base+3])<<24 |
				uint64(sigBytes[base+4])<<32 | uint64(sigBytes[base+5])<<40 |
				uint64(sigBytes[base+6])<<48 | uint64(sigBytes[base+7])<<56
		}

		if mode == 0 {
			limit := dstSpan
			if limit > 8 {
				limit = 8
			}
			for idx := 0; idx < limit; idx++ {
				v[dstStart+idx] ^= sigWords[idx]
			}
		} else {
			var total uint64
			for idx := 0; idx < 8; idx++ {
				total += uint64(bits.OnesCount64(sigWords[idx]))
			}
			v[dstStart] = total
		}
	}
}

// randomInstruction builds a packed instruction with random but valid operand
// fields. The 0..15 data window matches the tokens region in the test frame
// — every region (aSpan, bSpan, dstSpan) can span up to its full width and
// destinations are free to overlap source regions, which is exactly the
// interesting case for catching deferred-writeback bugs.
func randomInstruction(rng *rand.Rand) uint64 {
	const (
		dataMin = 0
		dataMax = 16
	)
	span := func() int { return rng.Intn(16) + 1 }
	start := func(span int) int {
		room := dataMax - span
		if room <= dataMin {
			return dataMin
		}
		return dataMin + rng.Intn(room+1)
	}
	aSpan := span()
	bSpan := span()
	dstSpan := span()
	return program.EncodeInstruction(
		start(aSpan), aSpan,
		start(bSpan), bSpan,
		start(dstSpan), dstSpan,
		uint64(rng.Intn(16)),
		uint64(rng.Intn(4)),
		uint64(rng.Intn(0x10000)),
	)
}

func TestUniversalBitwise_MatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(0xC0FFEE))

	for trial := 0; trial < 2000; trial++ {
		var asmFrame, refFrame [128]uint64

		// Random data in the first 16 words (the "tokens" region).
		for i := 0; i < 16; i++ {
			w := rng.Uint64()
			asmFrame[i] = w
			refFrame[i] = w
		}
		// 1..16 random instructions per trial (full program region).
		nInstr := rng.Intn(16) + 1
		for i := 0; i < nInstr; i++ {
			instr := randomInstruction(rng)
			asmFrame[16+i] = instr
			refFrame[16+i] = instr
		}

		UniversalBitwise(unsafe.Pointer(&asmFrame[0]))
		universalBitwiseRef(unsafe.Pointer(&refFrame[0]))

		if asmFrame != refFrame {
			t.Fatalf("trial %d (%d instrs):\n  asm: %x\n  ref: %x", trial, nInstr, asmFrame, refFrame)
		}
	}
}

func TestUniversalBitwise_NilIsSafe(t *testing.T) {
	UniversalBitwise(nil)
}

func TestUniversalBitwise_EmptyProgramIsNoop(t *testing.T) {
	var frame [128]uint64
	for i := 0; i < 16; i++ {
		frame[i] = uint64(i*7 + 1)
	}
	original := frame

	UniversalBitwise(unsafe.Pointer(&frame[0]))

	if frame != original {
		t.Fatalf("empty program mutated frame:\n  got:  %x\n  want: %x", frame, original)
	}
}

func BenchmarkUniversalBitwise(b *testing.B) {
	rng := rand.New(rand.NewSource(0xBEEF))

	var frame [128]uint64
	for i := 0; i < 16; i++ {
		frame[i] = rng.Uint64()
	}
	frame[16] = randomInstruction(rng)

	ptr := unsafe.Pointer(&frame[0])

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		UniversalBitwise(ptr)
	}
}
