package primitive

import (
	"math"
	"math/bits"
)

/*
AffinityWords is the number of uint64 words in an affinity vector.
257 bits requires 5 words: 4 full words (256 bits) plus 1 bit in word 4.
257 is a Fermat prime (2^(2^3) + 1), giving the affinity region algebraic
properties that composite widths lack — in particular, the multiplicative
group mod 257 has order 256 = 2^8, so every nonzero element has a power-of-2
order, making cyclic rotation and stride-based sampling degenerate-free for
any power-of-2 sub-sampling factor.
*/
const AffinityWords = 5

/*
AffinityBits is the number of meaningful bits in the affinity vector.
Bits beyond position 256 in the last word are always masked to zero.
*/
const AffinityBits = 257

/*
AffinityLastWordMask retains only bit 0 of the final affinity word,
zeroing the 63 unused bits. Every write path must apply this mask.
*/
const AffinityLastWordMask = uint64(1)

/*
RegionWords is the standard width of a 512-bit Value region (tokens,
program, signals, context, gradient, meta). Decoupled from AffinityWords
so the affinity can be a different width without breaking region accessors.
*/
const RegionWords = 8

/*
Affinity represents a locality-sensitive content fingerprint as a
fixed-width bit vector. Methods on Affinity provide Hamming distance,
population counting, EMA blending, and coupling strength — the full
vocabulary of affinity geometry that higher-level systems (DHT routing,
field projection) compose rather than re-implement.
*/
type Affinity struct {
	vector [AffinityWords]uint64
}

/*
NewAffinity constructs a zero-valued Affinity.
*/
func NewAffinity() *Affinity {
	return &Affinity{}
}

/*
NewAffinityFromVector constructs an Affinity from a raw bit vector.
*/
func NewAffinityFromVector(vector [AffinityWords]uint64) *Affinity {
	return &Affinity{vector: vector}
}

/*
Vector returns the raw bit vector. Callers that need read access
without copying can use this; mutation requires going through methods.
*/
func (affinity *Affinity) Vector() [AffinityWords]uint64 {
	return affinity.vector
}

/*
SetVector replaces the entire bit vector.
*/
func (affinity *Affinity) SetVector(vector [AffinityWords]uint64) {
	affinity.vector = vector
}

/*
Popcount returns the total number of set bits across the vector.
Used to detect saturation toward the Shannon limit where all
distance measurements become meaningless.
*/
func (affinity *Affinity) Popcount() int {
	total := 0

	for idx, word := range affinity.vector {
		if idx == AffinityWords-1 {
			word &= AffinityLastWordMask
		}

		total += bits.OnesCount64(word)
	}

	return total
}

/*
Blend updates the affinity using exponential moving average at the bit
level. Instead of OR (which saturates to all-ones), each new vector
shifts the centroid toward itself: bits that agree stay, bits that
disagree get a chance to flip proportional to 1/count. This keeps
the centroid representative of actual content rather than the union
of everything it ever saw.

The shannonLimit parameter caps the maximum popcount; if blending
would push past it the operation is reverted.
*/
func (affinity *Affinity) Blend(
	incoming *Affinity, count uint64, shannonLimit int,
) uint64 {
	nextCount := count + 1

	if nextCount <= 1 {
		affinity.vector = incoming.vector

		return nextCount
	}

	prev := affinity.vector

	for wordIdx := range AffinityWords {
		agree := affinity.vector[wordIdx] & incoming.vector[wordIdx]
		disagree := affinity.vector[wordIdx] ^ incoming.vector[wordIdx]

		selector := rotateSelector(nextCount, wordIdx)
		flipToIncoming := disagree & selector & incoming.vector[wordIdx]
		flipToZero := disagree & selector & ^incoming.vector[wordIdx]

		affinity.vector[wordIdx] = agree | flipToIncoming | (affinity.vector[wordIdx] & ^flipToZero & ^(disagree & selector))
	}

	affinity.vector[AffinityWords-1] &= AffinityLastWordMask

	if affinity.Popcount() >= shannonLimit {
		// Hard revert used to freeze the centroid forever once it brushed
		// the Shannon headroom. Prune highest bits first so saturated
		// clusters keep adapting instead of becoming routing dead-ends.
		headroom := shannonLimit * 9 / 10

		if headroom < 1 {
			headroom = 1
		}

		affinity.pruneUntilPopcountAtMost(headroom)

		if affinity.Popcount() >= shannonLimit {
			affinity.vector = prev

			return nextCount
		}
	}

	return nextCount
}

/*
pruneUntilPopcountAtMost clears set bits from the MSB downward until the
vector is within max bits or no bits remain. Deterministic order keeps
routing reproducible while freeing centroid capacity under saturation.
*/
func (affinity *Affinity) pruneUntilPopcountAtMost(max int) {
	for affinity.Popcount() > max {
		if !affinity.clearHighestSetBit() {
			return
		}
	}
}

/*
clearHighestSetBit removes the most significant set bit across the masked
affinity width. Returns false when the vector is already empty.
*/
func (affinity *Affinity) clearHighestSetBit() bool {
	for wordIdx := AffinityWords - 1; wordIdx >= 0; wordIdx-- {
		word := affinity.vector[wordIdx]

		if wordIdx == AffinityWords-1 {
			word &= AffinityLastWordMask
		}

		if word == 0 {
			continue
		}

		bitIdx := bits.Len64(word) - 1

		affinity.vector[wordIdx] &^= 1 << uint(bitIdx)
		affinity.vector[AffinityWords-1] &= AffinityLastWordMask

		return true
	}

	return false
}

/*
Coupling scores how strongly two affinity vectors overlap, normalized
to [0,1]. Uses Jaccard similarity: intersection / union. Returns 0
when both vectors are empty.
*/
func (affinity *Affinity) Coupling(other *Affinity) float64 {
	intersectionBits := 0
	unionBits := 0

	for wordIdx := range AffinityWords {
		aWord := affinity.vector[wordIdx]
		oWord := other.vector[wordIdx]

		if wordIdx == AffinityWords-1 {
			aWord &= AffinityLastWordMask
			oWord &= AffinityLastWordMask
		}

		intersectionBits += bits.OnesCount64(aWord & oWord)
		unionBits += bits.OnesCount64(aWord | oWord)
	}

	if unionBits == 0 {
		return 0
	}

	return float64(intersectionBits) / float64(unionBits)
}

/*
OrBlend performs a simple OR-blend of the incoming vector into this one.
Used during early accumulation before switching to EMA.
*/
func (affinity *Affinity) OrBlend(incoming *Affinity) {
	for wordIdx := range AffinityWords {
		affinity.vector[wordIdx] |= incoming.vector[wordIdx]
	}

	affinity.vector[AffinityWords-1] &= AffinityLastWordMask
}

/*
IsZero returns true when no bits are set in the vector.
*/
func (affinity *Affinity) IsZero() bool {
	for _, word := range affinity.vector {
		if word != 0 {
			return false
		}
	}

	return true
}

/*
ShannonEntropy computes the binary entropy of the affinity vector
treated as a Bernoulli source (fraction of set bits).
*/
func (affinity *Affinity) ShannonEntropy() float64 {
	totalBits := AffinityBits
	setBits := affinity.Popcount()

	if setBits == 0 || setBits == totalBits {
		return 0
	}

	prob := float64(setBits) / float64(totalBits)

	return -(prob*math.Log2(prob) + (1-prob)*math.Log2(1-prob))
}

/*
rotateSelector generates a pseudo-random bit mask where approximately 1/n
bits are set, used to select which disagreement bits flip toward the incoming
vector during blending. Uses a simple LCG seeded by count and word index.
*/
func rotateSelector(count uint64, wordIdx int) uint64 {
	if count == 0 {
		count = 1
	}

	seed := count*2654435761 + uint64(wordIdx)*1442695040888963407
	limit := (^uint64(0) / count) * count
	var mask uint64

	const maxRejects = 256

	for bit := 0; bit < 64; bit++ {
		var slot uint64

		rejects := 0

		for {
			seed = seed*6364136223846793005 + 1442695040888963407
			v := seed

			if v < limit {
				slot = v % count
				break
			}

			rejects++

			if rejects >= maxRejects {
				slot = v % count
				break
			}
		}

		if slot == 0 {
			mask |= 1 << uint(bit)
		}
	}

	return mask
}
