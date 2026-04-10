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
AffinityWithVector returns an Affinity by value so callers can take its address
for APIs that need *Affinity without a heap allocation (Publish/Store hot path).
*/
func AffinityWithVector(vector [AffinityWords]uint64) Affinity {
	return Affinity{vector: vector}
}

/*
AffinityVectorIsZero reports whether the vector is all-zero in the 257-bit sense
(last word is masked like AffinityVector).
*/
func AffinityVectorIsZero(vector [AffinityWords]uint64) bool {
	for wordIdx := range AffinityWords {
		word := vector[wordIdx]

		if wordIdx == AffinityWords-1 {
			word &= AffinityLastWordMask
		}

		if word != 0 {
			return false
		}
	}

	return true
}

/*
AffinityForNodeID folds a 64-bit mesh identity into the 257-bit affinity
space deterministically. Routing uses this as a stable pseudo-centroid
for a peer until richer learned affinities replace it, so Closest is not
driven purely by bucket insertion order.
*/
func AffinityForNodeID(nodeID uint64) *Affinity {
	var vec [AffinityWords]uint64

	vec[0] = nodeID
	vec[1] = bits.Reverse64(nodeID)
	vec[2] = nodeID ^ 0xaaaaaaaaaaaaaaaa
	vec[3] = nodeID ^ 0x5555555555555555
	vec[4] = nodeID & AffinityLastWordMask

	return NewAffinityFromVector(vec)
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
	total := bits.OnesCount64(affinity.vector[0]) +
		bits.OnesCount64(affinity.vector[1]) +
		bits.OnesCount64(affinity.vector[2]) +
		bits.OnesCount64(affinity.vector[3]) +
		int(affinity.vector[4]&AffinityLastWordMask)

	return total
}

/*
Blended returns the EMA-blended affinity and the next observation count.
It is the functional form of the blend: base is not mutated; incoming
must be non-nil for count≥1 paths. Shannon headroom handling matches the
previous in-place implementation (prune, then revert vector on failure).

Lock-free centroids (e.g. kadabra mesh load) compose this with CAS loops
instead of holding a mutex around a mutating Blend.

Next conversion target: Blend is still Go-scalar because its per-count
selector mask is built from a rejection-sampled LCG (see rotateSelector)
and then composed bitwise with agree / disagree masks. Moving this into
a substrate kernel requires staging the selector across the Value — most
likely the selector for a fixed-count slice is precomputed once at the
programmer layer, written into a reserved region, and consumed by a
tile program that emits the five output words via universalBitwiseV2
with the agree/disagree/selector triple fed as the ABC surfaces. The
prune fallback under Shannon saturation would remain Go-side until it
earns its own kernel, because it is only hit on the rare saturation path.
*/
func (base Affinity) Blended(
	incoming *Affinity, count uint64, shannonLimit int,
) (Affinity, uint64) {
	nextCount := count + 1

	var work Affinity

	work.vector = base.vector

	if nextCount <= 1 {
		if incoming != nil {
			work.vector = incoming.vector
		}

		work.vector[AffinityWords-1] &= AffinityLastWordMask

		return work, nextCount
	}

	if incoming == nil {
		work.vector[AffinityWords-1] &= AffinityLastWordMask

		return work, nextCount
	}

	prev := work.vector

	for wordIdx := range AffinityWords {
		agree := work.vector[wordIdx] & incoming.vector[wordIdx]
		disagree := work.vector[wordIdx] ^ incoming.vector[wordIdx]

		selector := rotateSelector(nextCount, wordIdx)
		flipToIncoming := disagree & selector & incoming.vector[wordIdx]
		flipToZero := disagree & selector & ^incoming.vector[wordIdx]

		work.vector[wordIdx] = agree | flipToIncoming |
			(work.vector[wordIdx] & ^flipToZero & ^(disagree & selector))
	}

	work.vector[AffinityWords-1] &= AffinityLastWordMask

	if work.Popcount() >= shannonLimit {
		headroom := max(shannonLimit*9/10, 1)

		work.pruneUntilPopcountAtMost(headroom)

		if work.Popcount() >= shannonLimit {
			work.vector = prev

			return work, nextCount
		}
	}

	return work, nextCount
}

/*
pruneUntilPopcountAtMost clears set bits from the MSB downward until the
vector is within max bits or no bits remain. Deterministic order keeps
routing reproducible while freeing centroid capacity under saturation.
*/
func (affinity *Affinity) pruneUntilPopcountAtMost(max int) {
	current := affinity.Popcount()

	for current > max {
		if !affinity.clearHighestSetBit() {
			return
		}

		current--
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
	intersection := bits.OnesCount64(affinity.vector[0]&other.vector[0]) +
		bits.OnesCount64(affinity.vector[1]&other.vector[1]) +
		bits.OnesCount64(affinity.vector[2]&other.vector[2]) +
		bits.OnesCount64(affinity.vector[3]&other.vector[3]) +
		int((affinity.vector[4]&other.vector[4])&AffinityLastWordMask)

	union := bits.OnesCount64(affinity.vector[0]|other.vector[0]) +
		bits.OnesCount64(affinity.vector[1]|other.vector[1]) +
		bits.OnesCount64(affinity.vector[2]|other.vector[2]) +
		bits.OnesCount64(affinity.vector[3]|other.vector[3]) +
		int((affinity.vector[4]|other.vector[4])&AffinityLastWordMask)

	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
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

	for bit := range 64 {
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
