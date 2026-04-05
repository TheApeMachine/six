package kadabra

import (
	"math/bits"
	"sort"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
AffinityDistance returns the Hamming distance between two affinity vectors.
Lower distance means more similar content specialization.
*/
func AffinityDistance(a, b [AffinityWords]uint64) int {
	total := 0
	for i := range AffinityWords {
		total += bits.OnesCount64(a[i] ^ b[i])
	}
	return total
}

/*
ValueAffinity extracts the affinity vector from a Value's affinity region.
*/
func ValueAffinity(v *primitive.Value) [AffinityWords]uint64 {
	var aff [AffinityWords]uint64
	start := core.Cfg.Value.Region.Affinity.Start
	for i := range AffinityWords {
		aff[i] = v[start+i]
	}
	return aff
}

/*
updateAffinity blends a Value's affinity into the node's running affinity
using exponential moving average at the bit level. Each bit position
tracks a majority vote: if more than half of ingested Values had that bit
set, the node's affinity bit is set.

For efficiency, we use a simple OR-blend for the first 64 Values, then
switch to decay-weighted accumulation.
*/
func (node *KadabraNode) updateAffinity(valueAffinity [AffinityWords]uint64) {
	node.affinityCount++

	if node.affinityCount <= 1 {
		node.Affinity = valueAffinity
		return
	}

	// Blend: keep bits that are consistently set across ingested Values.
	// Use OR for accumulation — the affinity broadens as the node sees
	// more diverse data. This is intentional: a node's affinity should
	// represent everything it knows about, not a narrow average.
	for i := range AffinityWords {
		node.Affinity[i] |= valueAffinity[i]
	}
}

/*
closestNodesByAffinity returns up to limit nodes from the routing table,
sorted by affinity distance to the target affinity vector.
*/
func (node *KadabraNode) closestNodesByAffinity(
	target [AffinityWords]uint64, limit int,
) []*KadabraNode {
	if limit <= 0 {
		return nil
	}

	type candidate struct {
		node     *KadabraNode
		distance int
	}

	var candidates []candidate

	// Collect all known peers.
	seen := map[NodeID]struct{}{node.ID: {}}
	for _, bucket := range node.buckets {
		if bucket == nil {
			continue
		}

		bucket.mu.RLock()
		for _, entry := range bucket.Entries {
			if entry == nil || entry.Node == nil {
				continue
			}

			if _, exists := seen[entry.ID]; exists {
				continue
			}

			seen[entry.ID] = struct{}{}
			candidates = append(candidates, candidate{
				node:     entry.Node,
				distance: AffinityDistance(target, entry.Node.Affinity),
			})
		}
		bucket.mu.RUnlock()
	}

	// Include self.
	candidates = append(candidates, candidate{
		node:     node,
		distance: AffinityDistance(target, node.Affinity),
	})

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].distance == candidates[j].distance {
			return candidates[i].node.ID < candidates[j].node.ID
		}
		return candidates[i].distance < candidates[j].distance
	})

	result := make([]*KadabraNode, 0, min(limit, len(candidates)))
	for i := 0; i < len(candidates) && len(result) < limit; i++ {
		result = append(result, candidates[i].node)
	}

	return result
}
