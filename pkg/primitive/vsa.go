package primitive

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
affinityProjections holds one prime stride per affinity output word
(see AffinityWords in affinity.go). ComputeAffinityLSH runs one SimHash-style
projection per word: each projection walks token bits with its stride and
majority-votes bit positions into that word. The last word only receives one
output bit, so the aggregate fingerprint is AffinityBits (257) meaningful bits,
not the width of the token region.

Strides are prime so indexing uses idx := ... % tokenBits without structural
degeneracy when tokenBits is a power of two (the usual case: token region is
512 bits). A composite stride sharing a factor with tokenBits can revisit only
a sub-lattice of indices and correlates projections that should be independent.

The slice length matches AffinityWords (currently {73, 97, 113, 131, 151});
when tokenBits is large (e.g. 512), lcm(p1,p2) for two distinct primes in this
range dwarfs tokenBits, so two projections do not realign within one token span.

Sensitivity: power-of-two strides align with bit-index parity classes; primes
that are too tight in range reduce geometric diversity between projections.
*/
var affinityProjections = [AffinityWords]int{73, 97, 113, 131, 151}

/*
affinityLSHSamplesPerBit controls how many token bits vote for each affinity
output bit via majority vote. Higher k stabilizes each bit but dilutes
discrimination across the fixed token span (core.Cfg.Value.Region.Tokens.Bits).

The default k was tuned when the token region is 512 bits: each output bit
samples a small, stride-dependent subset of those bits rather than the full
span. The affinity output width stays AffinityBits (257); this constant does
not enlarge the fingerprint, it only controls variance per output bit.

Sensitivity: very small k yields noisy fingerprints; very large k smears
differences between near-duplicate token patterns and hurts routing.
*/
const affinityLSHSamplesPerBit = 7

/*
ComputeAffinityLSH fills the configured affinity region from the token region:
one majority-vote projection per affinity word, using affinityProjections[stride
per word]. Output width follows Region.Affinity.Bits (with AffinityWords and
AffinityLastWordMask applied, typically 257 bits: four full uint64 lanes plus
one bit in the fifth word). Token bit count comes from Region.Tokens.Bits.
*/
func (value *Value) ComputeAffinityLSH() error {
	tokenBits := int(core.Cfg.Value.Region.Tokens.Bits)
	if tokenBits <= 0 {
		return errnie.Error(NewPrimitiveError(
			ErrPrimitiveInvalidValue,
			nil,
			"ComputeAffinityLSH",
		))
	}

	nWords := (tokenBits + 63) / 64
	tokStart := core.Cfg.Value.Region.Tokens.Start
	affStart := core.Cfg.Value.Region.Affinity.Start
	affWords := int(core.Cfg.Value.Region.Affinity.Bits+63) / 64

	for proj := 0; proj < affWords && proj < AffinityWords; proj++ {
		stride := affinityProjections[proj]

		outBits := 64
		if proj == AffinityWords-1 {
			outBits = 1
		}

		var word uint64

		for out := range outBits {
			ones, counted := 0, 0

			for s := range affinityLSHSamplesPerBit {
				idx := (out*stride + s*stride + proj*37) % tokenBits
				w := idx / 64

				if w >= nWords || tokStart+w >= core.Cfg.Value.Words {
					continue
				}

				counted++
				ones += int((value[tokStart+w] >> uint(idx%64)) & 1)
			}

			if counted > 0 && ones*2 >= counted {
				word |= 1 << uint(out)
			}
		}

		value[affStart+proj] = word
	}

	return nil
}

/*
ComputeAffinityFromContext sets the affinity region from raw bytes by OR-ing
set bits derived from 4-byte and 8-byte n-grams (see loop below). Each n-gram
hashes to a word index and bit index modulo AffinityWords / bits per word, so
the fingerprint spreads across the same AffinityBits-wide region as LSH, not
a separate 512-bit space.

Empty context falls back to ComputeAffinityLSH. This path targets byte-level
structure (substring overlap) where bit-majority over the token slab is a
poor match for natural text statistics.
*/
func (value *Value) ComputeAffinityFromContext(context []byte) error {
	if value == nil {
		return nil
	}

	if len(context) == 0 {
		return value.ComputeAffinityLSH()
	}

	affStart := core.Cfg.Value.Region.Affinity.Start
	affWords := int(core.Cfg.Value.Region.Affinity.Bits+63) / 64

	// Hash each n-gram to (aff word, bit) so load spreads across AffinityWords
	// instead of saturating a single 64-bit Bloom when context is long.
	var aff [AffinityWords]uint64

	for _, ngramWidth := range []int{4, 8} {
		if ngramWidth > len(context) {
			continue
		}

		for i := 0; i <= len(context)-ngramWidth; i++ {
			h := fnvHash(context[i : i+ngramWidth])
			wordIdx := h % uint64(AffinityWords)

			bitsPerWord := 64

			if wordIdx == uint64(AffinityWords-1) {
				bitsPerWord = 1
			}

			hashPart := h >> 6
			var bitIdx uint64

			if bitsPerWord == 64 {
				bitIdx = hashPart & 63
			} else {
				bitIdx = hashPart % uint64(bitsPerWord)
			}

			aff[wordIdx] |= 1 << uint(bitIdx)
		}
	}

	for wordIdx := 0; wordIdx < affWords && wordIdx < AffinityWords; wordIdx++ {
		(*value)[affStart+wordIdx] = aff[wordIdx]
	}

	if affStart+AffinityWords-1 < core.Cfg.Value.Words {
		(*value)[affStart+AffinityWords-1] &= AffinityLastWordMask
	}

	return nil
}

/*
ComputeAffinityBloom computes a 64-bit Bloom filter from overlapping
3-byte n-grams of the raw input data. Two Values sharing high
AND-popcount in their Affinity words are likely to share substrings.
*/
func ComputeAffinityBloom(data []byte) uint64 {
	if len(data) < 3 {
		if len(data) > 0 {
			return fnvBit(data)
		}
		return 0
	}
	var bloom uint64
	for i := 0; i <= len(data)-3; i++ {
		bloom |= fnvBit(data[i : i+3])
	}
	return bloom
}

/*
fnvHash computes a full 64-bit FNV-1a hash of a byte slice.
*/
func fnvHash(data []byte) uint64 {
	h := uint64(14695981039346656037)
	for _, b := range data {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return h
}

/*
fnvBit hashes a small byte slice to a single bit position (0..63)
using FNV-1a.
*/
func fnvBit(data []byte) uint64 {
	return 1 << (fnvHash(data) & 63)
}

/*
BloomOverlap returns the number of shared Bloom filter bits between two
64-bit Affinity words — equivalent to Popcount(A & B).
*/
func BloomOverlap(a, b uint64) int {
	return bits.OnesCount64(a & b)
}

/*
LFSRStep advances a 13-bit LFSR by one step, producing a maximal-length
cycle of 2^13 - 1 = 8191 states. Uses the primitive polynomial
x^13 + x^4 + x^3 + x + 1 (taps at bits 12, 3, 2, 0).
*/
func LFSRStep(state uint64) uint64 {
	if state == 0 {
		state = 1 // LFSR must never be all-zero
	}
	bit := ((state >> 12) ^ (state >> 3) ^ (state >> 2) ^ state) & 1
	state = ((state << 1) | bit) & 0x1FFF // mask to 13 bits
	return state
}

/*
LFSRAdvance advances the LFSR by n steps.
*/
func LFSRAdvance(state uint64, n int) uint64 {
	for range n {
		state = LFSRStep(state)
	}
	return state
}

/*
XORDistance returns the XOR distance between two 64-bit Affinity words.
In a Kademlia topology this defines a valid metric space.
*/
func XORDistance(a, b uint64) uint64 {
	return a ^ b
}

/*
XORDistanceLog returns the log2 of the XOR distance (the index of the
highest differing bit), used for Kademlia k-bucket placement.
Returns -1 if a == b.
*/
func XORDistanceLog(a, b uint64) int {
	d := a ^ b
	if d == 0 {
		return -1
	}
	return 63 - bits.LeadingZeros64(d)
}
