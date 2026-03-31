package primitive

import "math/bits"

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
	StartBit int    // absolute bit offset of the longest run
	Length   int    // length of the run in bits
	SourceA  uint64 // ValueID of operand A
	SourceB  uint64 // ValueID of operand B
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

	// We scan XOR (cancel) and AND (merge) — the two fundamental signal types.
	type scanSpec struct {
		op   uint8
		kind SignalKind
		// For cancel we look at zero-runs in XOR result.
		// For merge we look at one-runs in AND result.
		invertForScan bool // if true, count zero-runs; else count one-runs
	}
	specs := []scanSpec{
		{op: 0x6, kind: SignalCancel, invertForScan: true}, // XOR → zero-run
		{op: 0x1, kind: SignalMerge, invertForScan: false}, // AND → one-run
	}

	idA := a[57] // ValueID at word 57
	idB := b[57]

	var signals []Signal

	for _, spec := range specs {
		bestStart, bestLen := -1, 0
		var allRuns []Signal

		currentStart := -1
		currentLen := 0

		for w := 0; w < tokenWords; w++ {
			idx := baseIdx + w
			if idx >= Words {
				break
			}

			var result uint64
			switch spec.op {
			case 0x6:
				result = a[idx] ^ b[idx]
			case 0x1:
				result = a[idx] & b[idx]
			}

			// For cancel (XOR) we want zero-runs → invert so ones = zeros.
			scanWord := result
			if spec.invertForScan {
				scanWord = ^result
			}

			// Walk bits in this word, extending or closing runs.
			for bit := 0; bit < 64; bit++ {
				bitSet := (scanWord >> bit) & 1
				absBit := w*64 + bit

				if bitSet == 1 {
					if currentStart < 0 {
						currentStart = absBit
						currentLen = 1
					} else {
						currentLen++
					}
				} else {
					if currentLen > 0 {
						sig := Signal{
							Kind:     spec.kind,
							Op:       spec.op,
							StartBit: currentStart,
							Length:   currentLen,
							SourceA:  idA,
							SourceB:  idB,
						}
						allRuns = append(allRuns, sig)
						if currentLen > bestLen {
							bestLen = currentLen
							bestStart = currentStart
						}
						currentStart = -1
						currentLen = 0
					}
				}
			}
		}

		// Close any trailing run.
		if currentLen > 0 {
			sig := Signal{
				Kind:     spec.kind,
				Op:       spec.op,
				StartBit: currentStart,
				Length:   currentLen,
				SourceA:  idA,
				SourceB:  idB,
			}
			allRuns = append(allRuns, sig)
			if currentLen > bestLen {
				bestLen = currentLen
				bestStart = currentStart
			}
		}

		_ = bestStart // used to identify which is longest
		signals = append(signals, allRuns...)
	}

	// Sort by length descending so caller can easily separate longest from rest.
	sortSignals(signals)
	return signals
}

// sortSignals sorts signals by length descending (longest first).
// Uses insertion sort — signal count per pair is small.
func sortSignals(sigs []Signal) {
	for i := 1; i < len(sigs); i++ {
		key := sigs[i]
		j := i - 1
		for j >= 0 && sigs[j].Length < key.Length {
			sigs[j+1] = sigs[j]
			j--
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
