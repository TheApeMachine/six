package vm

import (
	"sort"

	"github.com/theapemachine/six/pkg/core/numeric/geometry"
)

/*
affinityVPThreshold switches from bucket scans to a vantage-point tree over
registered communities. Below this count, linear scans stay cheaper.
*/
const affinityVPThreshold = 32

/*
affinityVPNode is a Hamming-space vantage-point tree node. Internal nodes
carry a vantage affinity and split radius; leaves hold community fields.
*/
type affinityVPNode struct {
	vantage [5]uint64
	mu      int
	inner   *affinityVPNode
	outer   *affinityVPNode
	leaf    []*geometry.Field
}

func medianInt(values []int) int {
	if len(values) == 0 {
		return 0
	}

	sorted := append([]int(nil), values...)
	sort.Ints(sorted)

	return sorted[len(sorted)/2]
}

func buildAffinityVP(fields []*geometry.Field) *affinityVPNode {
	if len(fields) == 0 {
		return nil
	}

	if len(fields) <= 8 {
		return &affinityVPNode{leaf: append([]*geometry.Field(nil), fields...)}
	}

	var vantage [5]uint64

	copy(vantage[:], fields[0].Affinity[:5])

	dists := make([]int, len(fields))

	for idx, field := range fields {
		dists[idx] = geometry.AffinityHammingDistance(vantage[:], field.Affinity[:5])
	}

	mu := medianInt(dists)

	var inner []*geometry.Field

	var outer []*geometry.Field

	for idx, field := range fields {
		if dists[idx] < mu {
			inner = append(inner, field)
			continue
		}

		outer = append(outer, field)
	}

	if len(inner) == 0 || len(outer) == 0 {
		half := len(fields) / 2
		if half == 0 {
			return &affinityVPNode{leaf: append([]*geometry.Field(nil), fields...)}
		}

		inner = append([]*geometry.Field(nil), fields[:half]...)
		outer = append([]*geometry.Field(nil), fields[half:]...)
	}

	return &affinityVPNode{
		vantage: vantage,
		mu:      mu,
		inner:   buildAffinityVP(inner),
		outer:   buildAffinityVP(outer),
	}
}

/*
nearestCommunityWithin walks the tree and returns the closest community whose
live affinity lies within the Hamming budget (ties broken by first found at
leaves). Both subtrees are visited so merges that move affinities cannot hide
candidates behind a stale split.
*/
func nearestCommunityWithin(
	node *affinityVPNode,
	query [5]uint64,
	budget int,
) (*geometry.Field, int) {
	if node == nil {
		return nil, budget + 1
	}

	if len(node.leaf) > 0 {
		var best *geometry.Field

		bestDist := budget + 1

		for _, field := range node.leaf {
			if field == nil {
				continue
			}

			dist := geometry.AffinityHammingDistance(query[:], field.Affinity[:5])

			if dist <= budget && dist < bestDist {
				bestDist = dist
				best = field
			}
		}

		return best, bestDist
	}

	left, dLeft := nearestCommunityWithin(node.inner, query, budget)
	right, dRight := nearestCommunityWithin(node.outer, query, budget)

	if left == nil {
		return right, dRight
	}

	if right == nil {
		return left, dLeft
	}

	if dLeft <= dRight {
		return left, dLeft
	}

	return right, dRight
}
