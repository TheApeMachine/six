package primitive

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/core"
)

/*
signalAffineRing is the smallest prime above the 3648-bit token region so an
affine index map can permute scan order without folding through a composite
modulus.
*/
const signalAffineRing = 3659

/*
signalScanStrides are coprime to signalAffineRing; each stride defines a
different reordering of token bits before longest-run detection so cancel /
merge spans can align non-contiguous agreements as well as literal runs.
*/
var signalScanStrides = [...]uint64{2, 7, 11, 13, 17, 23}

type signalScanSpec struct {
	op            uint8
	kind          SignalKind
	invertForScan bool
}

// SignalKind classifies what a detected span implies.
type SignalKind uint8

const (
	// SignalCancel: XOR result with long zero-run → shared component
	// (the two sources partially overlap, the zero part is the shared structure).
	SignalCancel SignalKind = iota
	// SignalMerge: AND result with long one-run → convergence point
	// (the two sources agree on a dense region).
	SignalMerge
)

// Signal is a contiguous bit-span detected in the result of a bitwise
// operation between two Values. The substrate uses signals to decide
// what new Values (structure labels, edges) to emit.
type Signal struct {
	Kind     SignalKind
	Op       uint8  // truth-table opcode that produced the result
	StartBit int    // physical bit offset for linear scan (ScanStride==0); virtual step start when ScanStride!=0
	Length   int    // length of the run in bits
	SourceA  uint64 // ValueID of operand A
	SourceB  uint64 // ValueID of operand B
	// ScanStride selects affine order: physicalBit(step)=((ScanStride*step)%signalAffineRing)%tokenBits.
	// Zero means StartBit indexes a linear physical span (legacy ExtractSpan path).
	ScanStride uint64
}

// ScanSignals applies every relevant opcode between two Values' token regions,
// finds the longest contiguous zero-run (cancel) and one-run (merge) in each
// result, and returns ALL detected signals sorted longest-first per kind.
// The caller uses the longest as the local action and publishes the rest
// for inter-cluster exchange.
func ScanSignals(a, b *Value, tokenWords int, baseIdx int) []Signal {
	if tokenWords <= 0 || a == nil || b == nil {
		return nil
	}

	tokenBits := int(core.Cfg.Value.Region.Tokens.Bits)
	if tokenBits <= 0 {
		tokenBits = tokenWords * 64
	}

	// We scan XOR (cancel) and AND (merge) — the two fundamental signal types.
	specs := []signalScanSpec{
		{op: 0x6, kind: SignalCancel, invertForScan: true}, // XOR → zero-run
		{op: 0x1, kind: SignalMerge, invertForScan: false}, // AND → one-run
	}

	idWord := core.Cfg.Value.Region.ID.Start
	if idWord < 0 || idWord >= Words {
		return nil
	}

	idA := a[idWord]
	idB := b[idWord]

	opWords := make([]uint64, tokenWords)
	var signals []Signal

	for _, spec := range specs {
		for w := 0; w < tokenWords; w++ {
			idx := baseIdx + w
			if idx >= Words {
				break
			}

			switch spec.op {
			case 0x6:
				opWords[w] = a[idx] ^ b[idx]
			case 0x1:
				opWords[w] = a[idx] & b[idx]
			}
		}

		for _, stride := range signalScanStrides {
			signals = append(signals, scanSignalsAffineStride(opWords, spec, stride, tokenBits, idA, idB)...)
		}

		signals = append(signals, scanSignalsLinear(opWords, spec, tokenBits, idA, idB)...)
	}

	// Sort by length descending so caller can easily separate longest from rest.
	sortSignals(signals)
	return signals
}

func scanSignalsLinear(opWords []uint64, spec signalScanSpec, tokenBits int, idA, idB uint64) []Signal {
	return scanSignalsWithIndexFunc(
		opWords,
		spec,
		tokenBits,
		idA,
		idB,
		0,
		func(absBit int) int { return absBit },
	)
}

func scanSignalsAffineStride(opWords []uint64, spec signalScanSpec, stride uint64, tokenBits int, idA, idB uint64) []Signal {
	return scanSignalsWithIndexFunc(
		opWords,
		spec,
		tokenBits,
		idA,
		idB,
		stride,
		func(absBit int) int {
			return int((stride*uint64(absBit))%signalAffineRing) % tokenBits
		},
	)
}

func scanSignalsWithIndexFunc(
	opWords []uint64,
	spec signalScanSpec,
	tokenBits int,
	idA, idB uint64,
	scanStride uint64,
	bitIndexFunc func(absBit int) int,
) []Signal {
	var allRuns []Signal

	currentStart := -1
	currentLen := 0

	for absBit := 0; absBit < tokenBits; absBit++ {
		phys := bitIndexFunc(absBit)
		bitSet := int(readOpWordBit(opWords, phys))
		if spec.invertForScan {
			bitSet ^= 1
		}

		if bitSet == 1 {
			if currentStart < 0 {
				currentStart = absBit
				currentLen = 1
			} else {
				currentLen++
			}

			continue
		}

		if currentLen > 0 {
			allRuns = append(allRuns, Signal{
				Kind:       spec.kind,
				Op:         spec.op,
				StartBit:   currentStart,
				Length:     currentLen,
				SourceA:    idA,
				SourceB:    idB,
				ScanStride: scanStride,
			})
			currentStart = -1
			currentLen = 0
		}
	}

	if currentLen > 0 {
		allRuns = append(allRuns, Signal{
			Kind:       spec.kind,
			Op:         spec.op,
			StartBit:   currentStart,
			Length:     currentLen,
			SourceA:    idA,
			SourceB:    idB,
			ScanStride: scanStride,
		})
	}

	return allRuns
}

func readOpWordBit(opWords []uint64, phys int) uint64 {
	if phys < 0 {
		return 0
	}

	w := phys / 64
	b := uint(phys % 64)
	if w >= len(opWords) {
		return 0
	}

	return (opWords[w] >> b) & 1
}

/*
ExtractAffineRun packs length bits read from wordSrc along the affine scan
defined by stride and virtualStart (same convention as ScanSignals).
*/
func ExtractAffineRun(wordSrc []uint64, stride uint64, virtualStart, length, tokenBits int) []uint64 {
	if length <= 0 {
		return nil
	}

	nWords := (length + 63) / 64
	out := make([]uint64, nWords)

	for i := 0; i < length; i++ {
		step := virtualStart + i
		phys := int((stride*uint64(step))%signalAffineRing) % tokenBits
		if readOpWordBit(wordSrc, phys) != 0 {
			dw := i / 64
			db := uint(i % 64)
			out[dw] |= 1 << db
		}
	}

	return out
}

/*
ExtractSpanForSignal copies the bits described by a Signal from a linear token
word slice; affine strides use ExtractAffineRun so Structure extraction matches
how the run was found.
*/
func ExtractSpanForSignal(wordSrc []uint64, sig Signal, tokenBits int) []uint64 {
	if sig.ScanStride == 0 {
		return ExtractSpan(wordSrc, sig.StartBit, sig.Length)
	}

	return ExtractAffineRun(wordSrc, sig.ScanStride, sig.StartBit, sig.Length, tokenBits)
}

// sortSignals sorts signals by length descending (longest first),
// then by ScanStride ascending so physical (zero-stride) ties win over affine.
// Uses insertion sort — signal count per pair is small.
func sortSignals(sigs []Signal) {
	for i := 1; i < len(sigs); i++ {
		key := sigs[i]
		j := i - 1
		for j >= 0 {
			if sigs[j].Length < key.Length {
				sigs[j+1] = sigs[j]
				j--

				continue
			}

			if sigs[j].Length == key.Length && sigs[j].ScanStride > key.ScanStride {
				sigs[j+1] = sigs[j]
				j--

				continue
			}

			break
		}
		sigs[j+1] = key
	}
}

// SplitSignals separates signals into the longest per kind (local action)
// and the rest (for inter-cluster exchange). Returns (local, exchange).
func SplitSignals(signals []Signal) (local []Signal, exchange []Signal) {
	// Already sorted longest-first. Take the first of each kind as local.
	seenKind := [2]bool{}
	for _, sig := range signals {
		if !seenKind[sig.Kind] {
			seenKind[sig.Kind] = true
			local = append(local, sig)
		} else {
			exchange = append(exchange, sig)
		}
	}
	return
}

// LongestZeroRun returns the start bit and length of the longest contiguous
// zero-run across n uint64 words starting at base in the given array.
func LongestZeroRun(words []uint64, n int) (start, length int) {
	return longestRun(words, n, true)
}

// LongestOneRun returns the start bit and length of the longest contiguous
// one-run across n uint64 words.
func LongestOneRun(words []uint64, n int) (start, length int) {
	return longestRun(words, n, false)
}

func longestRun(words []uint64, n int, wantZero bool) (bestStart, bestLen int) {
	curStart := -1
	curLen := 0

	for w := 0; w < n && w < len(words); w++ {
		scan := words[w]
		if wantZero {
			scan = ^scan
		}

		if scan == ^uint64(0) {
			// Entire word matches.
			if curStart < 0 {
				curStart = w * 64
			}
			curLen += 64
			continue
		}
		if scan == 0 {
			// Entire word is empty — close any run.
			if curLen > bestLen {
				bestLen = curLen
				bestStart = curStart
			}
			curStart = -1
			curLen = 0
			continue
		}

		// Mixed word: walk individual bits.
		for bit := 0; bit < 64; bit++ {
			if (scan>>bit)&1 == 1 {
				if curStart < 0 {
					curStart = w*64 + bit
					curLen = 1
				} else {
					curLen++
				}
			} else {
				if curLen > bestLen {
					bestLen = curLen
					bestStart = curStart
				}
				curStart = -1
				curLen = 0
			}
		}
	}

	if curLen > bestLen {
		bestLen = curLen
		bestStart = curStart
	}
	return
}

// SpanPopcount returns the popcount of a bit range [startBit, startBit+length)
// within a word slice.
func SpanPopcount(words []uint64, startBit, length int) int {
	if length <= 0 {
		return 0
	}
	count := 0
	for i := 0; i < length; i++ {
		bit := startBit + i
		w := bit / 64
		b := uint(bit % 64)
		if w < len(words) && (words[w]>>b)&1 == 1 {
			count++
		}
	}
	return count
}

// ExtractSpan copies length bits starting at startBit from src into a new
// word slice, packed from bit 0. Used to extract the "shared component"
// from a cancel signal.
func ExtractSpan(src []uint64, startBit, length int) []uint64 {
	nWords := (length + 63) / 64
	out := make([]uint64, nWords)
	for i := 0; i < length; i++ {
		srcBit := startBit + i
		sw := srcBit / 64
		sb := uint(srcBit % 64)
		if sw < len(src) && (src[sw]>>sb)&1 == 1 {
			dw := i / 64
			db := uint(i % 64)
			out[dw] |= 1 << db
		}
	}
	return out
}

// PopcountSlice returns the total popcount of a word slice.
func PopcountSlice(words []uint64) int {
	n := 0
	for _, w := range words {
		n += bits.OnesCount64(w)
	}
	return n
}
