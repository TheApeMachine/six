package primitive

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
affinityProjections holds the prime strides used for 8 independent LSH
projections across the 512-bit affinity region. Each projection uses a
different stride to sample different subsets of the token bits.

All 8 values are prime numbers chosen from the range [73, 199]. Primes are
required because the stride is used modulo tokenBits (512) to index into the
input: a composite stride that shares a factor with 512 (any power of 2)
would create degenerate sampling patterns that revisit the same subset of
input bits, collapsing independent projections into correlated ones.

The specific primes {73, 97, 113, 131, 151, 167, 181, 199} are spread across
the range to maximize pairwise stride differences. This ensures that any two
projections sample maximally different subsets of input bits -- for a 512-bit
input, two projections with strides p1 and p2 first re-collide after
lcm(p1,p2) steps, and since both are prime, lcm = p1*p2 >> 512, meaning they
never re-collide within the input, producing fully independent samples.

Sensitivity: replacing any prime with a power-of-2 (e.g. 128) causes that
projection to sample only even-indexed or only odd-indexed bits, halving its
effective input and correlating it with other projections. Using primes that
are too close together (e.g. all in [71,89]) reduces the diversity of sampling
patterns, weakening the LSH's ability to distinguish inputs that differ only
in specific bit regions.
*/
var affinityProjections = [8]int{73, 97, 113, 131, 151, 167, 181, 199}

/*
affinityLSHSamplesPerBit controls how many token bits vote for each output
bit via majority vote. Higher values produce more stable (less noisy) output
bits but reduce sensitivity to small input differences.

7 is chosen for 512-bit token inputs based on the following reasoning:
  - Each output bit's majority vote has error rate ~ exp(-2 * margin^2 * k)
    where k is the sample count. At k=7, a true-positive input bit (>50%
    of sampled bits set) is correctly output with probability >96%.
  - At k=3 (too few), the majority vote flips on a single noisy input bit,
    making the affinity fingerprint unstable across minor input variations.
  - At k=15 (too many), the vote is extremely stable but each output bit
    averages over ~3% of the input, washing out fine-grained differences
    between similar-but-distinct inputs. Two inputs differing in only 10%
    of their token bits would produce nearly identical affinity vectors.
  - k=7 samples ~1.4% of the input per output bit, preserving sensitivity
    to ~5% input differences while suppressing single-bit noise.

Sensitivity: reducing to 3 makes affinity vectors noisy enough that
BloomOverlap / affineCoupling produce false positives on unrelated inputs.
Increasing to 15+ causes genuinely different inputs to hash to the same
affinity vector, creating false mode merges in the Kadabra field.
*/
const affinityLSHSamplesPerBit = 7

/*
ComputeAffinityLSH projects the token region into the full 512-bit affinity
region (8 words × 64 bits) using 8 independent SimHash projections. Each
projection uses a different prime stride to sample different subsets of the
token bits via majority vote.
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

	for proj := 0; proj < affWords && proj < 8; proj++ {
		stride := affinityProjections[proj]
		var word uint64

		for out := 0; out < 64; out++ {
			ones, counted := 0, 0

			for s := 0; s < affinityLSHSamplesPerBit; s++ {
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
ComputeAffinityFromContext writes the affinity region of a Value using an
arbitrary byte slice as the source signal, rather than the Value's own
token region. This allows affinity routing based on a larger context
window than the 64-byte token region can hold.

The algorithm is the same SimHash majority-vote projection as
ComputeAffinityLSH, but the input bits come from the supplied context
bytes instead of value[tokStart..].
*/
/*
ComputeAffinityFromContext writes the affinity region using multi-scale
n-gram Bloom fingerprints over an arbitrary byte slice. Each of the 8
affinity words captures a different n-gram scale (3..10 bytes), so the
512-bit fingerprint encodes structural patterns at multiple resolutions.

This is fundamentally different from the token-region SimHash (which
operates on raw bits and cannot distinguish ASCII text with similar
character distributions). N-gram Blooms capture sequential byte patterns
— "def " vs "fn " produce different 3-gram sets and therefore different
fingerprints.
*/
func ComputeAffinityFromContext(value *Value, context []byte) {
	if len(context) == 0 {
		_ = value.ComputeAffinityLSH()
		return
	}

	affStart := core.Cfg.Value.Region.Affinity.Start
	affWords := int(core.Cfg.Value.Region.Affinity.Bits+63) / 64

	// Compute n-gram hashes and distribute them across the 8 affinity
	// words using the hash value itself to select which word and bit.
	// This spreads the fingerprint across all 512 bits, avoiding the
	// saturation problem of per-word Bloom filters on large inputs.
	//
	// We use two n-gram scales (4 and 8 bytes) so the fingerprint
	// captures both keyword-level and phrase-level structure.
	var aff [8]uint64
	for _, ngramWidth := range []int{4, 8} {
		if ngramWidth > len(context) {
			continue
		}
		for i := 0; i <= len(context)-ngramWidth; i++ {
			h := fnvHash(context[i : i+ngramWidth])
			wordIdx := (h >> 6) & 7          // which of 8 words
			bitIdx := h & 63                  // which bit in that word
			aff[wordIdx] |= 1 << uint(bitIdx)
		}
	}

	for i := 0; i < affWords && i < 8; i++ {
		value[affStart+i] = aff[i]
	}
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
AdvanceSequence advances the Value's StateSequence word by one LFSR step.
*/
func (value *Value) AdvanceSequence() {
	value[core.Cfg.Value.Region.State.Sequence] = LFSRStep(
		value[core.Cfg.Value.Region.State.Sequence],
	)
}

/*
AccumulateDelta XORs the Tokens region of current and previous into the
StateAccumulator word. This captures the "change" between two states
as a compressed differential sketch.
*/
func AccumulateDelta(current, previous *Value) uint64 {
	nWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	base := core.Cfg.Value.Region.Tokens.Start
	var delta uint64
	for i := range nWords {
		idx := base + i
		if idx >= core.Cfg.Value.Words {
			break
		}
		delta ^= current[idx] ^ previous[idx]
	}
	current[core.Cfg.Value.Region.State.Accumulator] = delta
	return delta
}

/*
ApplyDelta generates a predicted next token region by XORing the current
Tokens with the accumulated delta stored in StateAccumulator.
*/
func ApplyDelta(dst, current *Value) {
	nWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	base := core.Cfg.Value.Region.Tokens.Start
	delta := current[core.Cfg.Value.Region.State.Accumulator]
	for i := range nWords {
		idx := base + i
		if idx >= core.Cfg.Value.Words {
			break
		}
		dst[idx] = current[idx] ^ delta
	}
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
