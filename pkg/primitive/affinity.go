package primitive

import (
	"math"
	"math/bits"
)

/*
AffinityWords is the number of uint64 words in an affinity vector.
*/
const AffinityWords = 8

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
Distance returns the Hamming distance between two affinity vectors.
Lower distance means more similar content specialization.
*/
func (affinity *Affinity) Distance(other *Affinity) int {
	total := 0

	for wordIdx := range AffinityWords {
		total += bits.OnesCount64(affinity.vector[wordIdx] ^ other.vector[wordIdx])
	}

	return total
}

/*
Popcount returns the total number of set bits across the vector.
Used to detect saturation toward the Shannon limit where all
distance measurements become meaningless.
*/
func (affinity *Affinity) Popcount() int {
	total := 0

	for _, word := range affinity.vector {
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
func (affinity *Affinity) Blend(incoming *Affinity, count uint64, shannonLimit int) {
	count++

	if count <= 1 {
		affinity.vector = incoming.vector
		return
	}

	prev := affinity.vector

	for wordIdx := range AffinityWords {
		agree := affinity.vector[wordIdx] & incoming.vector[wordIdx]
		disagree := affinity.vector[wordIdx] ^ incoming.vector[wordIdx]

		selector := rotateSelector(count, wordIdx)
		flipToIncoming := disagree & selector & incoming.vector[wordIdx]
		flipToZero := disagree & selector & ^incoming.vector[wordIdx]

		affinity.vector[wordIdx] = agree | flipToIncoming | (affinity.vector[wordIdx] & ^flipToZero & ^(disagree & selector))
	}

	if affinity.Popcount() >= shannonLimit {
		affinity.vector = prev
	}
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
		intersectionBits += bits.OnesCount64(affinity.vector[wordIdx] & other.vector[wordIdx])
		unionBits += bits.OnesCount64(affinity.vector[wordIdx] | other.vector[wordIdx])
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
	totalBits := AffinityWords * 64
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
	seed := count*2654435761 + uint64(wordIdx)*1442695040888963407
	var mask uint64

	for bit := 0; bit < 64; bit++ {
		seed = seed*6364136223846793005 + 1442695040888963407

		if seed%count == 0 {
			mask |= 1 << uint(bit)
		}
	}

	return mask
}
