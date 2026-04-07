package programmer

import (
	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
packNearestAffinityCandidates writes up to kernel.MaxNearestAffinityCandidates
raw affinity vectors contiguously from word 32 for nearest-affinity batch
kernels. Each vector uses primitive.AffinityWords uint64s. Returns how many
vectors were written (may be less than len(assets) when the frame is full).
*/
func packNearestAffinityCandidates(value *primitive.Value, assets [][]uint64) int {
	cursor := kernel.NearestAffinityCandidatesStartWord
	packed := 0

	for _, asset := range assets {
		if packed >= kernel.MaxNearestAffinityCandidates {
			break
		}

		nextCursor := cursor + primitive.AffinityWords

		if nextCursor > kernel.NearestAffinityBatchWord {
			break
		}

		for wordIdx := range primitive.AffinityWords {
			w := uint64(0)

			if wordIdx < len(asset) {
				w = asset[wordIdx]
			}

			value.Set(cursor+wordIdx, w)
		}

		cursor = nextCursor
		packed++
	}

	return packed
}

/*
applyBatchAffinityLayout packs nearest-affinity candidates into the Value
frame for opcode 0x6 batch kernels. Shared by CPU, Metal, and CUDA paths.
*/
func applyBatchAffinityLayout(value *primitive.Value, assets [][]uint64) {
	packed := packNearestAffinityCandidates(value, assets)

	if packed == 0 {
		for wordIdx := range primitive.AffinityWords {
			value.Set(32+wordIdx, 0)
		}

		packed = 1
	}

	value.Set(124, uint64(packed))
}
