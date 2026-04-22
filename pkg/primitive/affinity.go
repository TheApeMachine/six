package primitive

import "math/bits"

// NormalizeAffinityWords enforces the configured 257-bit affinity contract.
// The final affinity word contributes only its low bit; all higher bits are
// masked before routing, Jaccard coupling, or persistence.
func NormalizeAffinityWords(words []uint64) {
	if len(words) == 0 {
		return
	}

	words[len(words)-1] &= AffinityLastWordMask
}

func (value *Value) NormalizeAffinity() {
	if value == nil {
		return
	}

	NormalizeAffinityWords(value.Get(AffinityRegion))
}

func (value *Value) AffinityArray() [AffinityWords]uint64 {
	var out [AffinityWords]uint64
	if value == nil {
		return out
	}

	words := value.Get(AffinityRegion)
	for i := 0; i < len(out) && i < len(words); i++ {
		out[i] = words[i]
	}

	out[AffinityWords-1] &= AffinityLastWordMask
	return out
}

func AffinityBitCount(finger [AffinityWords]uint64) int {
	finger[AffinityWords-1] &= AffinityLastWordMask

	total := 0
	for _, word := range finger {
		total += bits.OnesCount64(word)
	}

	return total
}

func AffinityHamming(a, b [AffinityWords]uint64) int {
	a[AffinityWords-1] &= AffinityLastWordMask
	b[AffinityWords-1] &= AffinityLastWordMask

	total := 0
	for i := range a {
		total += bits.OnesCount64(a[i] ^ b[i])
	}

	return total
}

func AffinityJaccard(a, b [AffinityWords]uint64) float64 {
	a[AffinityWords-1] &= AffinityLastWordMask
	b[AffinityWords-1] &= AffinityLastWordMask

	inter, union := 0, 0
	for i := range a {
		inter += bits.OnesCount64(a[i] & b[i])
		union += bits.OnesCount64(a[i] | b[i])
	}

	if union == 0 {
		return 1
	}

	return float64(inter) / float64(union)
}
