package kernel

import (
	"math/bits"

	"github.com/theapemachine/six/pkg/primitive"
)

func RecruitmentSaturation(words []uint64, limit float64) bool {
	if limit <= 0 {
		limit = 0.47
	}

	total := 0
	for i, word := range words {
		if i == primitive.AffinityWords-1 {
			word &= primitive.AffinityLastWordMask
		}
		total += bits.OnesCount64(word)
	}

	return float64(total)/float64(primitive.AffinityBits) >= limit
}

func RecruitmentWouldSaturate(
	fold [primitive.AffinityWords]uint64,
	candidate [primitive.AffinityWords]uint64,
	limit float64,
) bool {
	var next [primitive.AffinityWords]uint64
	for i := range next {
		next[i] = fold[i] ^ candidate[i]
	}
	next[primitive.AffinityWords-1] &= primitive.AffinityLastWordMask
	return RecruitmentSaturation(next[:], limit)
}

func BatchRecruitmentSaturation(
	folds [][primitive.AffinityWords]uint64,
	candidates [][primitive.AffinityWords]uint64,
	limit float64,
) []bool {
	n := len(folds)
	if len(candidates) < n {
		n = len(candidates)
	}

	out := make([]bool, n)
	for i := 0; i < n; i++ {
		out[i] = RecruitmentWouldSaturate(folds[i], candidates[i], limit)
	}
	return out
}
