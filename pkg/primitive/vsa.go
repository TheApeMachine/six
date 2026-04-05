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
*/
var affinityProjections = [8]int{73, 97, 113, 131, 151, 167, 181, 199}

/*
affinityLSHSamplesPerBit controls how many token bits vote for each output
bit. Higher = more stable but less sensitive to small differences. 7 is a
good balance for 512 token bits: each output bit samples ~7 of 512 input
bits, giving a stable majority vote while keeping projections independent.
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
