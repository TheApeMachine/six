package resolve

import "github.com/theapemachine/six/pkg/numeric/geometry"

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
	ctxA := int32(context.CoordU)
	ctxB := int32(context.CoordV)

	for idx, node := range nodes {
		da := int64(node.CoordU) - int64(ctxA)
		db := int64(node.CoordV) - int64(ctxB)
		distSq64 := da*da + db*db
		if distSq64 > int64(maxPackedDistance) {
			distSq64 = int64(maxPackedDistance)
		}
		distSq := uint32(distSq64)

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


