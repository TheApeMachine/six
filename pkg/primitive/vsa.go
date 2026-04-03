package primitive

import (
	"math/bits"
	"math/rand"
	"sync"

	"github.com/theapemachine/six/pkg/core"
)

var (
	ByteSignatures [256][57]uint64
	vsaInitOnce    sync.Once
)

func initVSA() {
	vsaInitOnce.Do(func() {
		// Use a fixed seed so signatures are consistent across runs
		rng := rand.New(rand.NewSource(42))
		for i := range 256 {
			for w := range 57 {
				ByteSignatures[i][w] = rng.Uint64()
			}
		}
	})
}

func init() {
	initVSA()
}

/*
UnbindHD performs XOR unbinding: Residue = Fact ⊕ Query across the Tokens
region. In HCAM, if Fact = S⊕I⊕G and Query = S⊕I, then Residue = G.
The result is written into dst's Tokens region.
*/
func UnbindHD(dst, fact, query *Value) {
	nWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	base := core.Cfg.Value.Region.Tokens.Start
	for i := range nWords {
		idx := base + i
		if idx >= core.Cfg.Value.Words {
			break
		}
		dst[idx] = fact[idx] ^ query[idx]
	}
}

/*
BundleHD performs majority-rule bundling of multiple Tokens regions into dst.
For each bit position, the output is 1 if more than half the inputs have 1.
This is the VSA "superposition" operator.
*/
func BundleHD(dst *Value, sources []*Value) {
	if len(sources) == 0 {
		return
	}
	nWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	base := core.Cfg.Value.Region.Tokens.Start
	threshold := len(sources) / 2

	for i := range nWords {
		idx := base + i
		if idx >= core.Cfg.Value.Words {
			break
		}
		var result uint64
		for bit := range 64 {
			count := 0
			mask := uint64(1) << bit
			for _, src := range sources {
				if src[idx]&mask != 0 {
					count++
				}
			}
			if count > threshold {
				result |= mask
			}
		}
		dst[idx] = result
	}
}

/*
TokensHammingDistance returns the Hamming distance between two Values'
Tokens regions — the number of differing bits.
*/
func TokensHammingDistance(a, b *Value) int {
	nWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	base := core.Cfg.Value.Region.Tokens.Start
	dist := 0
	for i := range nWords {
		idx := base + i
		if idx >= core.Cfg.Value.Words {
			break
		}
		dist += bits.OnesCount64(a[idx] ^ b[idx])
	}
	return dist
}

/*
CosineSimilarityHD approximates cosine similarity for binary vectors via
(N - 2*HammingDistance) / N where N is the total bit width. Returns [-1, 1].
*/
func CosineSimilarityHD(a, b *Value) float64 {
	n := float64(core.Cfg.Value.Region.Tokens.Bits)
	if n == 0 {
		return 0
	}
	hd := float64(TokensHammingDistance(a, b))
	return (n - 2*hd) / n
}

/*
affinityLSHRing and affinityLSHStride spread SimHash samples across the whole
Tokens frame so shared prefixes no longer dominate the 64-bit Kademlia key.
*/
const affinityLSHRing = 3659
const affinityLSHStride = 73
const affinityLSHSamples = 57

/*
ComputeAffinityLSH projects the Tokens region into a 64-bit Affinity word:
for each output bit, majority vote over 57 samples at
(outBit*affinityLSHStride + step) mod affinityLSHRing — affine sampling
instead of contiguous blocks.
*/
func (value *Value) ComputeAffinityLSH() {
	tokenBits := int(core.Cfg.Value.Region.Tokens.Bits)
	nWords := int((core.Cfg.Value.Region.Tokens.Bits + 63) / 64)
	if nWords == 0 || tokenBits <= 0 {
		return
	}
	base := core.Cfg.Value.Region.Tokens.Start

	var affinity uint64
	for outBit := range 64 {
		ones := 0
		counted := 0

		for step := range affinityLSHSamples {
			idx := (outBit*affinityLSHStride + step) % affinityLSHRing
			if idx >= tokenBits {
				continue
			}

			w := idx / 64
			b := uint(idx % 64)
			wordIdx := base + w
			if wordIdx >= core.Cfg.Value.Words || w >= nWords {
				continue
			}

			counted++
			ones += int((value[wordIdx] >> b) & 1)
		}

		if counted > 0 && ones*2 >= counted {
			affinity |= 1 << uint(outBit)
		}
	}

	value[core.Cfg.Value.Region.Affinity.Start] = affinity
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
fnvBit hashes a small byte slice to a single bit position (0..63)
using FNV-1a.
*/
func fnvBit(data []byte) uint64 {
	h := uint64(14695981039346656037)
	for _, b := range data {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return 1 << (h & 63)
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
