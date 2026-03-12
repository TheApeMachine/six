package resolve

import "github.com/theapemachine/six/pkg/geometry"

const maxPackedDistance = 131072

/*
PackedNearest returns the packed nearest-neighbor result for a GF(257) rotation query.
*/
func PackedNearest(
	nodes []geometry.GFRotation,
	context geometry.GFRotation,
) uint64 {
	if len(nodes) == 0 {
		return 0
	}

	bestIdx := 0
	bestDistSq := uint32(maxPackedDistance)
	ctxA := int32(context.A)
	ctxB := int32(context.B)

	for idx, node := range nodes {
		da := int32(node.A) - ctxA
		db := int32(node.B) - ctxB
		distSq := uint32(da*da + db*db)

		if distSq < bestDistSq {
			bestIdx = idx
			bestDistSq = distSq
		}
	}

	return pack(bestIdx, bestDistSq)
}

func pack(idx int, distSq uint32) uint64 {
	if distSq > maxPackedDistance {
		distSq = maxPackedDistance
	}

	invertedDist := uint32(maxPackedDistance) - distSq
	return (uint64(invertedDist) << 32) | uint64(uint32(idx))
}
